package responses

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

func TestConvertOutputPreservesRefusalAlongsideText(t *testing.T) {
	var response Response
	require.NoError(t, json.Unmarshal([]byte(`{"output":[{"type":"message","content":[{"type":"output_text","text":"Visible."},{"type":"refusal","refusal":"Unable to help."}]}]}`), &response))
	message := convertOutputToMessage(response.Output, nil)
	require.Equal(t, "Unable to help.", message.Refusal)
	require.Equal(t, "Visible.", *message.Content.Content)
	reencoded := convertToResponsesAPIResponse(&llm.Response{Choices:[]llm.Choice{{Message:&message}}})
	require.Equal(t, "Unable to help.", convertOutputToMessage(reencoded.Output,nil).Refusal)
	history := convertAssistantMessage(message)
	require.Equal(t,"Unable to help.",convertOutputToMessage(history,nil).Refusal)
}

func TestRefusalStreamAndAggregation(t *testing.T) {
	delta := `{"type":"response.refusal.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":"Unable"}`
	done := `{"type":"response.refusal.done","item_id":"msg_1","output_index":0,"content_index":0,"refusal":"Unable to help."}`
	partDone := `{"type":"response.content_part.done","item_id":"msg_1","output_index":0,"content_index":0,"part":{"type":"refusal","refusal":"Unable to help."}}`
	itemDone := `{"type":"response.output_item.done","output_index":0,"item":{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"refusal","refusal":"Unable to help."}]}}`
	terminal := `{"type":"response.completed","response":{"id":"resp_test","model":"test","status":"completed","output":[],"usage":{"input_tokens":7,"output_tokens":3,"total_tokens":10}}}`
	for _, tc := range []struct{name string; events []string; terminal string}{
		{name:"delta and repeated snapshots",events:[]string{delta,done,done,partDone,itemDone}},
		{name:"done only",events:[]string{done}},
		{name:"part done only",events:[]string{partDone}},
		{name:"item done only",events:[]string{itemDone}},
		{name:"terminal only",terminal:`{"type":"response.completed","response":{"id":"resp_test","model":"test","status":"completed","output":[{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"refusal","refusal":"Unable to help."}]}]}}`},
	} {
		t.Run(tc.name,func(t *testing.T){
			source := []*httpclient.StreamEvent{
				{Data:[]byte(`{"type":"response.created","response":{"id":"resp_test","model":"test","status":"in_progress"}}`)},
				{Data:[]byte(`{"type":"response.output_item.added","output_index":0,"item":{"id":"msg_1","type":"message","role":"assistant"}}`)},
				{Data:[]byte(`{"type":"response.content_part.added","item_id":"msg_1","output_index":0,"content_index":0,"part":{"type":"refusal","refusal":""}}`)},
			}
			for _, raw := range tc.events { source=append(source,&httpclient.StreamEvent{Data:[]byte(raw)}) }
			last := terminal
			if tc.terminal != "" { last=tc.terminal }
			source=append(source,&httpclient.StreamEvent{Data:[]byte(last)})
			stream := newResponsesOutboundStream(streams.SliceStream(source))
			defer stream.Close()
			results,err := streams.All(stream)
			require.NoError(t,err)
			refusal := ""
			for _, result := range results { for _, choice := range result.Choices { if choice.Delta != nil { refusal+=choice.Delta.Refusal } } }
			require.Equal(t,"Unable to help.",refusal)
			require.True(t,stream.hasGeneratedOutput())
			nativeStream,err := NewInboundTransformer().TransformStream(t.Context(),streams.SliceStream(results))
			require.NoError(t,err)
			defer nativeStream.Close()
			nativeEvents,err := streams.All(nativeStream)
			require.NoError(t,err)
			nativeBody,_,err := AggregateStreamChunks(t.Context(),nativeEvents)
			require.NoError(t,err)
			var nativeResponse Response
			require.NoError(t,json.Unmarshal(nativeBody,&nativeResponse))
			require.Equal(t,"Unable to help.",convertOutputToMessage(nativeResponse.Output,nil).Refusal)
			body,_,err := AggregateStreamChunks(t.Context(),source)
			require.NoError(t,err)
			var aggregated Response
			require.NoError(t,json.Unmarshal(body,&aggregated))
			require.Equal(t,"Unable to help.",convertOutputToMessage(aggregated.Output,nil).Refusal)
		})
	}
}
