package responses

import (
 "fmt"
 "net/http"
 "net/http/httptest"
 "sync/atomic"
 "testing"
 "time"
 "context"

 "github.com/gorilla/websocket"
 "github.com/stretchr/testify/require"
 "github.com/looplj/axonhub/llm/httpclient"
 "github.com/looplj/axonhub/llm/transformer/shared"
)

func TestWebSocketExecutorInterruptBadAcknowledgementRetiresSocket(t *testing.T) {
 for _, mode := range []string{"missing", "wrong_id"} {
  t.Run(mode,func(t *testing.T) {
   var connections atomic.Int32
   server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request) {
    n := connections.Add(1)
    conn,err := (&websocket.Upgrader{}).Upgrade(w,r,nil)
    require.NoError(t,err)
    defer conn.Close()
    require.NoError(t,conn.SetReadDeadline(time.Now().Add(5*time.Second)))
    var request map[string]any
    require.NoError(t,conn.ReadJSON(&request))
    if n == 1 {
     require.NoError(t,conn.WriteMessage(websocket.TextMessage,[]byte(`{"type":"response.created","response":{"id":"resp_cancel","status":"in_progress"}}`)))
     require.NoError(t,conn.ReadJSON(&request))
     require.Equal(t,"response.interrupt",request["type"])
     require.NoError(t,conn.WriteMessage(websocket.TextMessage,[]byte(`{"type":"response.incomplete","response":{"id":"resp_cancel","status":"incomplete","incomplete_details":{"reason":"interrupted"},"output":[],"usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}}}`)))
     if mode == "wrong_id" { require.NoError(t,conn.WriteMessage(websocket.TextMessage,[]byte(`{"type":"response.interrupt.accepted","response_id":"foreign"}`))) }
     require.Error(t,conn.ReadJSON(&request),"unacknowledged socket must close before another request")
     return
    }
    require.NoError(t,conn.WriteMessage(websocket.TextMessage,[]byte(`{"type":"response.completed","response":{"id":"resp_next","status":"completed","output":[]}}`)))
   }))
   defer server.Close()
   executor := NewWebSocketExecutor(nil)
   control := shared.NewResponsesWebSocketSteering(2)
   ctx,cancel := context.WithTimeout(shared.WithResponsesWebSocketSteer(webSocketTestContext(),control),5*time.Second)
   defer cancel()
   request := &httpclient.Request{Method:http.MethodPost,URL:server.URL+"/v1/responses",Body:[]byte(`{"model":"gpt-6.1-sol","input":"hello"}`)}
   stream,err := executor.DoStream(ctx,request)
   require.NoError(t,err)
   require.True(t,stream.Next())
   require.True(t,control.Send([]byte(`{"type":"response.interrupt","response_id":"resp_cancel","mode":"discard_partial_items"}`)))
   require.True(t,stream.Next())
   require.Equal(t,"response.incomplete",stream.Current().Type)
   require.Contains(t,string(stream.Current().Data),`"total_tokens":5`)
   require.False(t,stream.Next())
   require.NoError(t,stream.Err())
   require.NoError(t,stream.Close())
   next,err := executor.DoStream(ctx,request)
   require.NoError(t,err)
   defer next.Close()
   require.True(t,next.Next(),fmt.Sprint(next.Err()))
   require.Equal(t,"response.completed",next.Current().Type)
   require.False(t,next.Next())
   require.Equal(t,int32(2),connections.Load())
  })
 }
}
