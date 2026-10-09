package responses

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

func TestCodex162NestedRetryAdvice(t *testing.T) {
	for _, tc := range []struct{ name, nested, want string }{
		{"seconds", `{"Retry-After":"7","Set-Cookie":"private"}`, "7"},
		{"zero", `{"Retry-After":0}`, "0"},
		{"long", `{"Retry-After":"7200"}`, "7200"},
		{"null", `null`, "12"},
		{"scalar", `"invalid"`, "12"},
		{"invalid", `{"retry-after":"bad"}`, "12"},
		{"newline", `{"retry-after":"\n5\n"}`, "12"},
		{"duplicate case", `{"Retry-After":"5","retry-after":"30"}`, "12"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"type":"error","status":429,"error":{"code":"rate_limit_exceeded","message":"wait","headers":%s},"headers":{"retry-after":"12"}}`, tc.nested)
			var event StreamEvent
			require.NoError(t, json.Unmarshal([]byte(body), &event))
			var raw *httpclient.Error
			require.ErrorAs(t, responseErrorFromStreamEvent(&event), &raw)
			require.Equal(t, tc.want, raw.Headers.Get("Retry-After"))
			require.Empty(t, raw.Headers.Get("Set-Cookie"))
		})
	}
}

func TestCodex162FailedResponseAdviceSurvivesConversion(t *testing.T) {
	for _, started := range []bool{false, true} {
		t.Run(fmt.Sprint(started), func(t *testing.T) {
			events := []*httpclient.StreamEvent{}
			if started {
				events = append(events, &httpclient.StreamEvent{Type: "response.created", Data: []byte(`{"type":"response.created","response":{"id":"resp_retry","model":"gpt-6.1-sol","status":"in_progress","output":[]}}`)})
			}
			events = append(events, &httpclient.StreamEvent{Type: "response.failed", Data: []byte(`{"type":"response.failed","response":{"id":"resp_retry","status":"failed","error":{"code":"server_is_overloaded","message":"retry later","headers":{"Retry-After":"7","Authorization":"private"}}}}`)})
			outbound, err := NewOutboundTransformer("https://api.example/v1", "synthetic-key")
			require.NoError(t, err)
			unified, err := outbound.TransformStream(t.Context(), &httpclient.Request{}, streams.SliceStream(events))
			require.NoError(t, err)
			wire, err := NewInboundTransformer().TransformStream(t.Context(), unified)
			require.NoError(t, err)
			failures := 0
			for wire.Next() {
				event := wire.Current()
				if event.Type == "response.failed" {
					failures++
					require.Equal(t, "7", gjson.GetBytes(event.Data, "response.error.headers.retry-after").String())
					require.Equal(t, "server_is_overloaded", gjson.GetBytes(event.Data, "response.error.code").String())
					require.NotContains(t, string(event.Data), "private")
				}
			}
			var failure *llm.ResponseError
			require.True(t, errors.As(wire.Err(), &failure))
			require.Equal(t, 503, failure.StatusCode)
			require.Equal(t, 1, failures)
			require.NoError(t, wire.Close())
		})
	}
}

func TestCodex162MisalignmentReviewTargetSurvivesConversion(t *testing.T) {
	detail := `{"error_type":"unauthorized_data_transfer","review_target":" RB/opaque== ","detailed_explanation":"provider explanation","steer":{"message":"keep files local"}}`
	var response Response
	require.NoError(t, json.Unmarshal([]byte(`{"status":"failed","error":{"code":"misalignment_policy_violation","message":"blocked","status":403,"misalignment":`+detail+`}}`), &response))
	failure := responseErrorFromResponse(&response)
	require.Equal(t, 403, failure.StatusCode)
	require.JSONEq(t, detail, string(failure.Detail.Misalignment))
	wire := NewInboundTransformer().TransformError(t.Context(), failure)
	require.JSONEq(t, detail, gjson.GetBytes(wire.Body, "error.misalignment").Raw)
	require.NotContains(t, failure.Error(), "RB/opaque")
}
