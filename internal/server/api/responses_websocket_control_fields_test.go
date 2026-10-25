package api

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResponsesWebSocketRejectsAmbiguousControlFields(t *testing.T) {
	for _, key := range []string{"type", "stream_id", "response_id", "mode", "input", "previous_response_id"} {
		for _, variant := range []string{key, strings.ToUpper(key), `\u` + fmt.Sprintf("%04x", key[0]) + key[1:]} {
			t.Run(key+"/"+variant, func(t *testing.T) {
				body := fmt.Sprintf(`{"%s":"first","%s":"second"`, key, variant)
				if key != "type" {
					body += `,"type":"response.create"`
				}
				_, _, err := responsesWebSocketEnvelope([]byte(body + `}`))
				require.NotNil(t, err)
			})
		}
		_, _, err := responsesWebSocketEnvelope([]byte(fmt.Sprintf(`{"type":"response.create","%s":"ambiguous"}`, strings.ToUpper(key))))
		require.NotNil(t, err)
	}

	// User/tool JSON is opaque: only the routing envelope has canonical controls.
	streamID, typ, err := responsesWebSocketEnvelope([]byte(`{"type":"response.create","stream_id":"lane-1","input":[{"type":"message","content":"ok","Type":"nested","input":{"mode":1,"mode":2}}]}`))
	require.Nil(t, err)
	require.Equal(t, "lane-1", streamID)
	require.Equal(t, responseCreateWebSocketEventType, typ)
}

func TestResponsesWebSocketAmbiguousInterruptDoesNotReachUpstream(t *testing.T) {
	d := &responsesWebSocketDispatcher{responseLanes: make(map[string]*responsesWebSocketLane)}
	lane := &responsesWebSocketLane{}
	control := lane.begin("gpt-6.1-sol")
	control.Activate()
	t.Cleanup(lane.end)
	d.registerResponseID("owned", lane)
	for _, field := range []string{
		`"response_id":"foreign","response_id":"owned"`,
		`"response_id":"foreign","response_\u0069d":"owned"`,
		`"response_id":"owned","Response_ID":"foreign"`,
		`"response_id":"owned","mode":"unknown"`,
		`"response_id":"owned","Mode":"unknown"`,
	} {
		payload := []byte(`{"type":"response.interrupt","mode":"discard_partial_items",` + field + `}`)
		require.NotNil(t, d.routeInterrupt(payload))
		require.Empty(t, control.Events())
		require.Empty(t, d.pendingInterrupts)
	}
	valid := []byte(`{"type":"response.interrupt","response_id":"owned","mode":"discard_partial_items"}`)
	require.Nil(t, d.routeInterrupt(valid))
	require.JSONEq(t, string(valid), string(<-control.Events()))
}
