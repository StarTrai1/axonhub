package orchestrator

import (
	"bytes"
	"context"

	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

// A successful HTTP open can still contain a Responses rejection. Keep only
// empty lifecycle events private until output starts, so an opening rejection
// follows the same bounded recovery as an HTTP error. Unknown events commit the
// attempt conservatively; partial summaries must never be generated twice.
func readLocalCompactionStreamOpening(ctx context.Context, stream streams.Stream[*httpclient.StreamEvent]) (streams.Stream[*httpclient.StreamEvent], error) {
	var buffered []*httpclient.StreamEvent
	bufferedBytes := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !stream.Next() {
			if err := stream.Err(); err != nil {
				return nil, err
			}
			return streams.PrependStream(stream, buffered...), nil
		}
		event := stream.Current()
		if event != nil {
			// Streams may reuse their event buffers on Next. Retain an owned copy
			// and read Current once, including for wrappers that advance on Current.
			owned := *event
			owned.Data = bytes.Clone(event.Data)
			event = &owned
		}
		buffered = append(buffered, event)
		if event != nil {
			if localCompactionEventHasOutput(event) {
				return streams.PrependStream(stream, buffered...), nil
			}
			if err := responses.StreamEventError(event); err != nil {
				return nil, err
			}
			kind := gjson.GetBytes(event.Data, "type").String()
			if kind == "" {
				kind = event.Type
			}
			switch kind {
			case "response.created", "response.queued", "response.in_progress":
			default:
				return streams.PrependStream(stream, buffered...), nil
			}
			bufferedBytes += len(event.Data)
		}
		if len(buffered) > 128 || bufferedBytes > 1<<20 {
			return nil, pipeline.ErrPreCommitBufferExceeded
		}
	}
}

func localCompactionEventHasOutput(event *httpclient.StreamEvent) bool {
	for _, path := range []string{"response.output", "output"} {
		output := gjson.GetBytes(event.Data, path)
		if output.Exists() && output.Type != gjson.Null && (!output.IsArray() || len(output.Array()) > 0) {
			return true
		}
	}
	for _, path := range []string{"item", "part", "delta", "response.usage.output_tokens", "usage.output_tokens"} {
		value := gjson.GetBytes(event.Data, path)
		if value.Exists() && value.Type != gjson.Null && (value.Type != gjson.Number || value.Int() > 0) {
			return true
		}
	}
	return false
}
