package orchestrator

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

// The assembled checkpoint is produced without an extra model call. Segment
// generation has its own persisted requests/usage; this local finalization has
// explicit zero usage and never invokes a provider executor.
type unifiedCompactionExecutor struct { summary string }

func (e *unifiedCompactionExecutor) response(request *httpclient.Request) ([]byte,json.RawMessage,error) {
	item,err:=json.Marshal(map[string]any{"type":"message","id":"msg_history_"+historyDigest([]byte(e.summary))[:20],"role":"assistant","status":"completed","content":[]map[string]string{{"type":"output_text","text":e.summary}}})
	if err!=nil{return nil,nil,err}
	body,err:=json.Marshal(map[string]any{"id":"resp_history_"+uuid.NewString(),"object":"response","status":"completed","model":gjson.GetBytes(request.Body,"model").String(),"output":[]json.RawMessage{item},"usage":map[string]int{"input_tokens":0,"output_tokens":0,"total_tokens":0}})
	return body,item,err
}

func (e *unifiedCompactionExecutor) Do(ctx context.Context,request *httpclient.Request)(*httpclient.Response,error){
	if err:=ctx.Err();err!=nil{return nil,err}
	body,_,err:=e.response(request);if err!=nil{return nil,err}
	return &httpclient.Response{StatusCode:http.StatusOK,Headers:http.Header{"Content-Type":{"application/json"},"X-Axonhub-History-Assembly":{"true"}},Body:body},nil
}

func (e *unifiedCompactionExecutor) DoStream(ctx context.Context,request *httpclient.Request)(streams.Stream[*httpclient.StreamEvent],error){
	if err:=ctx.Err();err!=nil{return nil,err}
	body,item,err:=e.response(request);if err!=nil{return nil,err}
	created,_:=json.Marshal(map[string]any{"type":"response.created","response":map[string]any{"id":gjson.GetBytes(body,"id").String(),"object":"response","status":"in_progress","model":gjson.GetBytes(body,"model").String(),"output":[]any{}}})
	added,_:=json.Marshal(map[string]any{"type":"response.output_item.added","output_index":0,"item":map[string]any{"type":"message","id":gjson.GetBytes(item,"id").String(),"role":"assistant","status":"in_progress","content":[]any{}}})
	done,_:=json.Marshal(map[string]any{"type":"response.output_item.done","output_index":0,"item":item})
	completed,_:=json.Marshal(map[string]any{"type":"response.completed","response":json.RawMessage(body)})
	return streams.SliceStream([]*httpclient.StreamEvent{{Type:"response.created",Data:created},{Type:"response.output_item.added",Data:added},{Type:"response.output_item.done",Data:done},{Type:"response.completed",Data:completed}}),nil
}
