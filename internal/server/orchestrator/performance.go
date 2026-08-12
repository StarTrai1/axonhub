package orchestrator

import (
	"context"
	"errors"
	"time"

	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/streams"
)

const errorMatchBodyLimit = 8 * 1024

// withPerformanceRecording creates a unified middleware that handles all performance tracking.
// It initializes metrics, tracks first token in streams, and records final metrics.
func withPerformanceRecording(outbound *PersistentOutboundTransformer) pipeline.Middleware {
	return &performanceRecording{
		outbound: outbound,
	}
}

// performanceRecording is a unified middleware that handles all performance tracking.
type performanceRecording struct {
	pipeline.DummyMiddleware

	outbound *PersistentOutboundTransformer
}

func (m *performanceRecording) Name() string {
	return "record-performance"
}

func (m *performanceRecording) OnInboundLlmRequest(ctx context.Context, request *llm.Request) (*llm.Request, error) {
	if m.outbound.state.Perf == nil {
		m.outbound.state.Perf = &biz.PerformanceRecord{}
	}

	if request.Stream != nil {
		m.outbound.state.Perf.Stream = *request.Stream
	} else {
		m.outbound.state.Perf.Stream = false
	}

	return request, nil
}

func (m *performanceRecording) OnOutboundRawRequest(ctx context.Context, request *httpclient.Request) (*httpclient.Request, error) {
	// Initialize performance metrics at the start of request
	channel := m.outbound.GetCurrentChannel()
	if channel == nil {
		return request, nil
	}

	// Preserve Stream flag from existing PerformanceRecord (set in OnInboundLlmRequest)
	var streamFlag bool
	if m.outbound.state.Perf != nil {
		streamFlag = m.outbound.state.Perf.Stream
	}

	// Create a new PerformanceRecord instance for each request.
	perf := biz.PerformanceRecord{}
	perf.StartTime = time.Now()
	perf.ChannelID = channel.ID
	perf.Success = false
	perf.RequestCompleted = false
	perf.Stream = streamFlag

	// Get the API key used for this request from context (set by TraceStickyKeyProvider).
	// OAuth channels authenticate from Credentials.OAuth and never pass through the
	// key provider, so they carry no context key. Identify them by the fixed OAuth
	// credential ref instead, which lets auto-disable and scheduled recovery treat an
	// OAuth channel as an ordinary one-credential channel.
	if apiKey, ok := contexts.GetChannelAPIKey(ctx); ok {
		perf.APIKey = apiKey
	} else if channel.Credentials.IsOAuth() {
		perf.APIKey = objects.OAuthCredentialRef
	}

	m.outbound.state.Perf = &perf

	log.Debug(ctx, "Started performance tracking",
		log.Int("channel_id", channel.ID),
		log.String("channel_name", channel.Name),
	)

	return request, nil
}

func (m *performanceRecording) OnOutboundRawResponse(ctx context.Context, response *httpclient.Response) (*httpclient.Response, error) {
	return response, nil
}

func (m *performanceRecording) OnOutboundLlmResponse(ctx context.Context, response *llm.Response) (*llm.Response, error) {
	if m.outbound.state.Perf == nil {
		return response, nil
	}

	if response != nil && response.Usage != nil {
		if tokenCount := response.Usage.GetCompletionTokens(); tokenCount != nil && *tokenCount > 0 {
			m.outbound.state.Perf.CompletionTokens = *tokenCount
		}
	}

	m.outbound.state.Perf.MarkSuccess()
	m.outbound.state.ChannelService.AsyncRecordPerformance(ctx, m.outbound.state.Perf)

	return response, nil
}

func (m *performanceRecording) OnOutboundRawStream(ctx context.Context, stream streams.Stream[*httpclient.StreamEvent]) (streams.Stream[*httpclient.StreamEvent], error) {
	return stream, nil
}

func (m *performanceRecording) OnOutboundLlmStream(ctx context.Context, stream streams.Stream[*llm.Response]) (streams.Stream[*llm.Response], error) {
	return &recordPerformanceStream{
		ctx:    ctx,
		stream: stream,
		state:  m.outbound.state,
	}, nil
}

func (m *performanceRecording) OnOutboundRawError(ctx context.Context, err error) {
	// Record performance metrics for failed requests
	if m.outbound.state.Perf == nil {
		return
	}

	perf := m.outbound.state.Perf
	if errors.Is(err, context.Canceled) {
		perf.MarkCanceled()
	} else {
		errorCode := ExtractErrorCode(err)
		perf.MarkFailedWithMessage(errorCode, extractErrorMessageForMatching(err))
	}

	m.outbound.state.ChannelService.AsyncRecordPerformance(ctx, perf)
}

// recordPerformanceStream records performance metrics for a stream of responses.
//
//nolint:containedctx // ctx is used for logging.
type recordPerformanceStream struct {
	ctx    context.Context
	stream streams.Stream[*llm.Response]
	state  *PersistenceState

	firstTokenSet     bool
	reasoningStartSet bool
	reasoningEndSet   bool
}

func (s *recordPerformanceStream) Current() *llm.Response {
	event := s.stream.Current()
	if event == nil {
		return event
	}

	if !s.firstTokenSet && s.state.Perf != nil && hasStreamOutput(event) {
		s.state.Perf.MarkFirstToken()
		s.firstTokenSet = true
	}

	if s.state.Perf != nil && len(event.Choices) > 0 {
		delta := event.Choices[0].Delta
		if delta != nil {
			if delta.ReasoningContent != nil && *delta.ReasoningContent != "" {
				if !s.reasoningStartSet {
					s.state.Perf.MarkReasoningStart()
					s.reasoningStartSet = true
				}
			} else if (delta.Content.Content != nil && *delta.Content.Content != "") || len(delta.Content.MultipleContent) > 0 || len(delta.ToolCalls) > 0 {
				if s.reasoningStartSet && !s.reasoningEndSet {
					s.state.Perf.MarkReasoningEnd()
					s.reasoningEndSet = true
				}
			}
		}
	}

	if tokenCount := event.Usage.GetCompletionTokens(); tokenCount != nil && *tokenCount > 0 {
		s.state.Perf.CompletionTokens = *tokenCount
		s.markSuccess()
	}

	// A Responses-compatible provider may omit usage from response.completed.
	// Its transformer still emits the unified [DONE] event, which is the protocol
	// completion boundary and must finalize latency before persistence runs.
	if event == llm.DoneResponse || event.Object == "[DONE]" {
		s.markSuccess()
	}

	return event
}

// hasStreamOutput reports whether an event carries model output that a client
// can consume. Protocol preambles, role-only chunks, usage frames, signatures,
// and terminal markers must not start TTFT.
func hasStreamOutput(response *llm.Response) bool {
	if response == nil || response == llm.DoneResponse || response.Object == "[DONE]" {
		return false
	}

	if response.SpeechStreamEvent != nil && response.SpeechStreamEvent.AudioBase64 != "" {
		return true
	}
	if response.SpeechAudioChunk != nil && len(response.SpeechAudioChunk.Audio) > 0 {
		return true
	}
	if response.TranscriptionStreamEvent != nil &&
		(response.TranscriptionStreamEvent.Delta != "" || response.TranscriptionStreamEvent.Text != "") {
		return true
	}

	for _, choice := range response.Choices {
		if hasStreamMessageOutput(choice.Delta) || hasStreamMessageOutput(choice.Message) {
			return true
		}
	}

	return false
}

func hasStreamMessageOutput(message *llm.Message) bool {
	if message == nil {
		return false
	}

	if message.Content.Content != nil && *message.Content.Content != "" {
		return true
	}
	for _, part := range message.Content.MultipleContent {
		if part.Text != nil && *part.Text != "" {
			return true
		}
		if part.ImageURL != nil && part.ImageURL.URL != "" {
			return true
		}
		if part.VideoURL != nil && part.VideoURL.URL != "" {
			return true
		}
		if part.Document != nil && part.Document.URL != "" {
			return true
		}
	}

	for _, toolCall := range message.ToolCalls {
		if toolCall.Function.Arguments != "" ||
			(toolCall.ResponseCustomToolCall != nil && toolCall.ResponseCustomToolCall.Input != "") {
			return true
		}
	}
	for _, result := range message.InlineToolResults {
		if result.Output != "" || result.IsError {
			return true
		}
	}
	if message.Refusal != "" {
		return true
	}
	if message.ReasoningContent != nil && *message.ReasoningContent != "" {
		return true
	}
	if message.Reasoning != nil && *message.Reasoning != "" {
		return true
	}
	for _, item := range message.ReasoningItems {
		if item.Content != "" {
			return true
		}
	}
	if message.Audio != nil && (message.Audio.Data != "" || message.Audio.Transcript != "") {
		return true
	}

	return false
}

func (s *recordPerformanceStream) markSuccess() {
	if s.state == nil || s.state.Perf == nil || s.state.Perf.RequestCompleted {
		return
	}

	s.state.Perf.MarkSuccess()
	if s.state.ChannelService != nil {
		s.state.ChannelService.AsyncRecordPerformance(s.ctx, s.state.Perf)
	}
}

func (s *recordPerformanceStream) Next() bool {
	return s.stream.Next()
}

func (s *recordPerformanceStream) Close() error {
	return s.stream.Close()
}

func (s *recordPerformanceStream) Err() error {
	return s.stream.Err()
}

// ExtractErrorCode extracts HTTP error code from error.
func ExtractErrorCode(err error) int {
	// Check if error is an HTTP error
	httpErr := &httpclient.Error{}
	if errors.As(err, &httpErr) {
		code := httpErr.StatusCode
		return code
	}

	// Default to 500
	return 500
}

func extractErrorMessageForMatching(err error) string {
	message := ExtractErrorMessage(err)
	httpErr := &httpclient.Error{}
	if !errors.As(err, &httpErr) || len(httpErr.Body) == 0 {
		return message
	}

	body := httpErr.Body
	if len(body) > errorMatchBodyLimit {
		body = body[:errorMatchBodyLimit]
	}

	return message + "\n" + string(body)
}

type NoopPerformanceRecording struct {
	pipeline.DummyMiddleware
}

func (m *NoopPerformanceRecording) Name() string {
	return "noop-performance"
}
