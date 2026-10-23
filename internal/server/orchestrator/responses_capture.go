package orchestrator

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

type responsesCompletion struct {
	mu           sync.Mutex
	body         []byte
	bytes        int
	overflow     bool
	budget       *shared.ResponsesBudget
	retained     int
	metadataOnly bool
}

// An acknowledged interrupt retains its response ID and only the output items
// present in its authoritative terminal body. Other incomplete outcomes do not
// establish a usable continuation, and this is not evidence of task completion.
func responsesBodyCanContinue(body gjson.Result) bool {
	return body.Get("id").String() != "" && body.Get("error").Type == gjson.Null &&
		(body.Get("status").String() == "completed" ||
			body.Get("status").String() == "incomplete" && body.Get("incomplete_details.reason").String() == "interrupted")
}

func (s *responsesCompletion) observe(event *httpclient.StreamEvent) {
	if event == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bytes += len(event.Data)
	if s.bytes > responsesSessionMaxResponse {
		s.overflow = true
	}
	typ := gjson.GetBytes(event.Data, "type").String()
	if typ == "response.completed" || typ == "response.incomplete" {
		body := gjson.GetBytes(event.Data, "response")
		if responsesBodyCanContinue(body) {
			if !s.overflow && !s.metadataOnly && s.budget != nil {
				if s.budget.Replace(s.retained, len(body.Raw)) {
					s.retained = len(body.Raw)
				} else {
					s.overflow = true
				}
			}
			if s.overflow || s.metadataOnly {
				s.body, _ = json.Marshal(struct {
					ID                string          `json:"id"`
					Status            string          `json:"status"`
					IncompleteDetails json.RawMessage `json:"incomplete_details,omitempty"`
				}{body.Get("id").String(), body.Get("status").String(), json.RawMessage(body.Get("incomplete_details").Raw)})
			} else {
				s.body = []byte(body.Raw)
			}
		}
	}
}

type responsesProviderStream struct {
	streams.Stream[*httpclient.StreamEvent]

	completion *responsesCompletion
	current    *httpclient.StreamEvent
}

func (s *responsesProviderStream) Next() bool {
	if !s.Stream.Next() {
		s.current = nil
		return false
	}
	s.current = s.Stream.Current()
	s.completion.observe(s.current)
	return true
}

func (s *responsesProviderStream) Current() *httpclient.StreamEvent { return s.current }

func captureResponsesProvider(p *PersistentOutboundTransformer) pipeline.Middleware {
	return &responsesProviderCapture{state: p.state}
}

type responsesProviderCapture struct {
	pipeline.DummyMiddleware

	state *PersistenceState
}

func (m *responsesProviderCapture) Name() string { return "responses_provider_completion" }

func (m *responsesProviderCapture) OnOutboundRawResponse(ctx context.Context, response *httpclient.Response) (*httpclient.Response, error) {
	if c := m.state.ResponsesContinuation; c != nil {
		c.capture.mu.Lock()
		c.capture.body = response.Body
		c.capture.overflow = len(response.Body) > responsesSessionMaxResponse
		if !c.capture.overflow && c.capture.budget != nil {
			if c.capture.budget.Replace(0, len(response.Body)) {
				c.capture.retained = len(response.Body)
			} else {
				c.capture.overflow = true
			}
		}
		c.capture.mu.Unlock()
	}
	return response, nil
}

func (m *responsesProviderCapture) OnOutboundRawStream(ctx context.Context, stream streams.Stream[*httpclient.StreamEvent]) (streams.Stream[*httpclient.StreamEvent], error) {
	if c := m.state.ResponsesContinuation; c != nil {
		return &responsesProviderStream{Stream: stream, completion: c.capture}, nil
	}
	return stream, nil
}

func (c *responsesContinuation) accept(ctx context.Context) {
	c.capture.mu.Lock()
	defer c.capture.mu.Unlock()
	body := c.capture.body
	if len(body) == 0 || !json.Valid(body) || !responsesBodyCanContinue(gjson.ParseBytes(body)) {
		return
	}
	id := gjson.GetBytes(body, "id").String()
	if id == "" {
		return
	}
	origin := shared.ResponsesOrigin{}
	if attempt := shared.GetResponsesAttempt(ctx); attempt != nil {
		origin = attempt.Origin()
	}
	if lane := shared.GetResponsesLane(ctx); lane != nil {
		c.lane = lane
		record := shared.ResponsesRecord{ID: id, Origin: origin}
		if origin.Format != "openai/responses-ws" && c.retain && !c.capture.overflow {
			retained := c.capture.retained == 0 || c.capture.retained == len(body)
			if c.capture.retained == len(body) {
				c.retentionBytes += c.capture.retained
				c.capture.retained = 0
			}
			var response struct {
				Output []json.RawMessage `json:"output"`
			}
			if retained && json.Unmarshal(body, &response) == nil {
				record.Snapshot = &shared.ResponsesSnapshot{Input: c.input, Output: response.Output}
			}
		}
		c.releaseRetention()
		if c.capture.budget != nil {
			c.capture.budget.Replace(c.capture.retained, 0)
			c.capture.retained = 0
		}
		lane.Remember(record)
	} else if c.store != nil {
		requestBody := c.effective
		if c.complete {
			requestBody = c.snapshotBody
		}
		if c.capture.overflow {
			requestBody = []byte(`{"previous_response_id":"unavailable"}`)
		}
		c.store.record(ctx, requestBody, body)
		if c.capture.budget != nil {
			c.capture.budget.Replace(c.capture.retained, 0)
			c.capture.retained = 0
		}
	}
	c.capture.body = nil
}

func (c *responsesContinuation) releaseCapture() {
	c.capture.mu.Lock()
	defer c.capture.mu.Unlock()
	if c.capture.budget != nil {
		c.capture.budget.Replace(c.capture.retained, 0)
	}
	c.capture.retained = 0
	c.capture.body = nil
}

type responsesAcceptedStream struct {
	streams.Stream[*httpclient.StreamEvent]

	ctx          context.Context
	continuation *responsesContinuation
	current      *httpclient.StreamEvent
}

func (s *responsesAcceptedStream) Next() bool {
	if !s.Stream.Next() {
		s.current = nil
		return false
	}
	s.current = s.Stream.Current()
	if _, deliveryTracked := s.Stream.(streams.DeliveryAcknowledger); !deliveryTracked && classifyStreamTerminalEvent(s.current) == streamTerminalCompleted {
		s.continuation.accept(s.ctx)
	}
	return true
}

func (s *responsesAcceptedStream) Current() *httpclient.StreamEvent { return s.current }

func (s *responsesAcceptedStream) Close() error {
	defer s.continuation.releaseRetention()
	defer s.continuation.releaseCapture()
	return s.Stream.Close()
}

func (s *responsesAcceptedStream) AcknowledgeDelivery() {
	if observer, ok := s.Stream.(streams.DeliveryAcknowledger); ok {
		observer.AcknowledgeDelivery()
		if observer.DeliveryAcknowledged() {
			s.continuation.accept(s.ctx)
		}
	}
}

func (s *responsesAcceptedStream) DeliveryAcknowledged() bool {
	observer, ok := s.Stream.(streams.DeliveryAcknowledger)
	return ok && observer.DeliveryAcknowledged()
}

func (s *responsesAcceptedStream) WaitForFinalization() {
	if waiter, ok := s.Stream.(streams.FinalizationWaiter); ok {
		waiter.WaitForFinalization()
	}
}

func (s *responsesAcceptedStream) Deliver(ctx context.Context, write func() (bool, error)) error {
	boundary, ok := s.Stream.(streams.DeliveryBoundary)
	if ok {
		err := boundary.Deliver(ctx, write)
		if err == nil && s.DeliveryAcknowledged() {
			s.continuation.accept(s.ctx)
		}
		return err
	}
	terminal, err := write()
	if err == nil && terminal {
		s.AcknowledgeDelivery()
	}
	return err
}

func (s *responsesAcceptedStream) ExpectedStreamChoices() int {
	choices, ok := s.Stream.(interface{ ExpectedStreamChoices() int })
	if !ok {
		return 0
	}
	return choices.ExpectedStreamChoices()
}
