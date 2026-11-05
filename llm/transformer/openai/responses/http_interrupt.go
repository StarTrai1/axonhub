package responses

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"

	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

// DoHTTPStream supports downstream Responses interrupts over an HTTP upstream.
// Only the transport context is cancelled; the pipeline still receives an
// incomplete result with the completed items, and can persist it for replay.
// No generation is resubmitted and no response.steer capability is advertised.
func DoHTTPStream(ctx context.Context, executor pipeline.Executor, request *httpclient.Request) (streams.Stream[*httpclient.StreamEvent], error) {
	control, ok := shared.GetResponsesWebSocketSteer(ctx)
	if !ok || control == nil || !shared.IsResponsesWebSocket(ctx) {
		return executor.DoStream(ctx, request)
	}
	transportCtx, cancel := context.WithCancel(ctx)
	source, err := executor.DoStream(transportCtx, request)
	if err != nil {
		cancel()
		return nil, err
	}
	s := &httpInterruptStream{
		source: source, ctx: ctx, cancel: cancel, control: control,
		completed: make(map[int]json.RawMessage), partial: make(map[int]string),
	}
	s.unregister = control.RegisterHTTPInterrupt(s.interrupt)
	return httpclient.WithResponseHeaders(s, httpclient.GetResponseHeaders(source)), nil
}

type httpInterruptStream struct {
	source      streams.Stream[*httpclient.StreamEvent]
	ctx         context.Context
	cancel      context.CancelFunc
	control     *shared.ResponsesWebSocketSteering
	unregister  func()
	closeOnce   sync.Once
	closeErr    error
	mu          sync.Mutex
	response    map[string]json.RawMessage
	responseID  string
	completed   map[int]json.RawMessage
	partial     map[int]string
	bytes       int
	unsafe      bool
	sequence    int64
	interrupted bool
	terminal    bool
	closed      bool
	queue       []*httpclient.StreamEvent
	current     *httpclient.StreamEvent
}

func (s *httpInterruptStream) interrupt(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.terminal || s.unsafe || id == "" || id != s.responseID || s.ctx.Err() != nil {
		return false
	}
	s.interrupted = true
	s.cancel()
	return true
}

func (s *httpInterruptStream) Next() bool {
	s.mu.Lock()
	if s.closed || s.ctx.Err() != nil {
		s.mu.Unlock()
		return false
	}
	if s.interrupted {
		ok := s.nextInterrupted()
		s.mu.Unlock()
		return ok
	}
	s.mu.Unlock()

	// Cancelling the transport unblocks a pending HTTP read without cancelling
	// the downstream lane or another multiplexed response.
	hasNext := s.source.Next()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.ctx.Err() != nil {
		return false
	}
	if s.interrupted {
		return s.nextInterrupted()
	}
	if !hasNext {
		return false
	}
	s.current = s.source.Current()
	s.observe(s.current)
	return true
}

func (s *httpInterruptStream) observe(event *httpclient.StreamEvent) {
	if event == nil {
		return
	}
	raw := gjson.ParseBytes(event.Data)
	s.sequence = max(s.sequence, raw.Get("sequence_number").Int())
	typ := raw.Get("type").String()
	if typ == "" {
		typ = event.Type
	}
	switch typ {
	case "response.created", "response.in_progress":
		if s.unsafe {
			return
		}
		response := raw.Get("response")
		id := response.Get("id").String()
		if id == "" || len(response.Raw) > 1<<20 || (s.responseID != "" && id != s.responseID) {
			s.unsafe = true
			return
		}
		s.responseID = id
		if json.Unmarshal([]byte(response.Raw), &s.response) != nil {
			s.unsafe = true
		}
	case "response.output_item.added", "response.output_item.done":
		if s.unsafe {
			return
		}
		index := raw.Get("output_index")
		item := raw.Get("item")
		if !index.Exists() || index.Int() < 0 || index.Int() >= 4096 || !item.IsObject() {
			s.unsafe = true
			return
		}
		i := int(index.Int())
		if typ == "response.output_item.added" {
			id := item.Get("id").String()
			if id == "" || s.completed[i] != nil {
				s.unsafe = true
				return
			}
			s.bytes += len(id) - len(s.partial[i])
			if s.bytes > 16<<20 {
				s.unsafe = true
				return
			}
			s.partial[i] = strings.Clone(id)
			return
		}
		if status := item.Get("status").String(); status != "" && status != "completed" {
			s.unsafe = true
			return
		}
		if previous := s.completed[i]; previous != nil && !bytes.Equal(previous, []byte(item.Raw)) {
			s.unsafe = true
			return
		}
		s.bytes += len(item.Raw) - len(s.completed[i]) - len(s.partial[i])
		if s.bytes > 16<<20 {
			s.unsafe = true
			return
		}
		s.completed[i] = json.RawMessage(item.Raw)
		delete(s.partial, i)
	case "response.completed", "response.incomplete", "response.failed", "response.cancelled", "error":
		s.terminal = true
		s.control.RememberTerminalResponse(s.responseID)
	}
}

// nextInterrupted runs under mu. A cancellation which wins before a source
// event is consumed discards that event. Natural completion which wins first
// remains authoritative and is acknowledged by the normal terminal cache.
func (s *httpInterruptStream) nextInterrupted() bool {
	if !s.terminal {
		s.terminal = true
		s.enqueue(map[string]any{"type": "response.interrupt.accepted", "response_id": s.responseID})
		indices := make([]int, 0, len(s.partial))
		for i := range s.partial {
			indices = append(indices, i)
		}
		slices.Sort(indices)
		for _, i := range indices {
			s.enqueue(map[string]any{"type": "response.output_item.interrupted", "response_id": s.responseID, "output_index": i, "item_id": s.partial[i]})
		}
		indices = indices[:0]
		for i := range s.completed {
			indices = append(indices, i)
		}
		slices.Sort(indices)
		output := make([]json.RawMessage, 0, len(indices))
		for _, i := range indices {
			output = append(output, s.completed[i])
		}
		outputJSON, _ := json.Marshal(output)
		s.response["output"] = outputJSON
		s.response["status"] = json.RawMessage(`"incomplete"`)
		s.response["incomplete_details"] = json.RawMessage(`{"reason":"interrupted"}`)
		// The upstream has not reported final usage. Do not invent token counts.
		s.enqueue(map[string]any{"type": "response.incomplete", "response": s.response})
		s.control.RememberTerminalResponse(s.responseID)
	}
	if len(s.queue) == 0 {
		return false
	}
	s.current = s.queue[0]
	s.queue = s.queue[1:]
	return true
}

func (s *httpInterruptStream) enqueue(event map[string]any) {
	s.sequence++
	event["sequence_number"] = s.sequence
	data, _ := json.Marshal(event)
	s.queue = append(s.queue, &httpclient.StreamEvent{Type: event["type"].(string), Data: data})
}

func (s *httpInterruptStream) Current() *httpclient.StreamEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current
}

func (s *httpInterruptStream) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ctx.Err(); err != nil {
		return err
	}
	if s.interrupted {
		return nil
	}
	return s.source.Err()
}

func (s *httpInterruptStream) Close() error {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.cancel()
		s.mu.Unlock()
		s.unregister()
		s.closeErr = s.source.Close()
	})
	return s.closeErr
}
