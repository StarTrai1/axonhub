package orchestrator

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/klauspost/compress/zstd"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/streams"
)

const nativeCompactionMaxSourceBytes = 64 << 20

type nativeCompactionSourceCheckpoint struct {
	Version int             `json:"version"`
	Body    json.RawMessage `json:"body"`
}

func nativeCompactionSourceAAD(ref *remoteCompactionReference, state *PersistenceState) ([]byte, error) {
	owner, err := localCompactionAssociatedData(ref, state)
	if err != nil {
		return nil, err
	}
	// Bind to both ID and ciphertext and separate source contexts from summaries.
	return fmt.Appendf(owner, ":native-source:%s", remoteCompactionCacheKey(ref)), nil
}

func (a *remoteCompactionAdapter) retainNativeCompactionSource(ctx context.Context, ref *remoteCompactionReference, state *PersistenceState, source *remoteCompactionSource) error {
	if a.systemService == nil || source == nil || len(source.body) == 0 || len(source.body) > nativeCompactionMaxSourceBytes {
		return errors.New("native compaction source storage is unavailable or source exceeds limit")
	}
	encoded, err := json.Marshal(nativeCompactionSourceCheckpoint{Version: 1, Body: source.body})
	if err != nil {
		return err
	}
	compressor, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1))
	if err != nil {
		return err
	}
	compressed := compressor.EncodeAll(encoded, nil)
	_ = compressor.Close()
	associatedData, err := nativeCompactionSourceAAD(ref, state)
	if err != nil {
		return err
	}
	aead, err := a.localCompactionCipher(ctx)
	if err != nil {
		return err
	}
	sealed, err := sealLocalCompactionSummary(aead, associatedData, base64.RawStdEncoding.EncodeToString(compressed))
	if err != nil {
		return fmt.Errorf("seal native compaction source: %w", err)
	}
	return a.systemService.SaveNativeCompactionSource(ctx, state.APIKey.ProjectID, state.APIKey.ID, remoteCompactionCacheKey(ref), sealed)
}

func (a *remoteCompactionAdapter) loadNativeCompactionSource(ctx context.Context, ref *remoteCompactionReference, state *PersistenceState) (*remoteCompactionSource, error) {
	if a.systemService == nil || state == nil || state.APIKey == nil {
		return nil, nil
	}
	sealed, err := a.systemService.LoadNativeCompactionSource(ctx, state.APIKey.ProjectID, state.APIKey.ID, remoteCompactionCacheKey(ref))
	if err != nil || sealed == "" {
		return nil, err
	}
	associatedData, err := nativeCompactionSourceAAD(ref, state)
	if err != nil {
		return nil, err
	}
	aead, err := a.localCompactionCipher(ctx)
	if err != nil {
		return nil, err
	}
	packed, err := openLocalCompactionSummary(aead, associatedData, sealed)
	if err != nil {
		return nil, err
	}
	compressed, err := base64.RawStdEncoding.DecodeString(packed)
	if err != nil {
		return nil, err
	}
	decoder, err := zstd.NewReader(nil, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(nativeCompactionMaxSourceBytes+(1<<20)))
	if err != nil {
		return nil, err
	}
	defer decoder.Close()
	decoded, err := decoder.DecodeAll(compressed, nil)
	if err != nil || len(decoded) > nativeCompactionMaxSourceBytes+(1<<20) {
		return nil, errors.New("invalid or oversized native compaction source")
	}
	var checkpoint nativeCompactionSourceCheckpoint
	if err := json.Unmarshal(decoded, &checkpoint); err != nil || checkpoint.Version != 1 || len(checkpoint.Body) > nativeCompactionMaxSourceBytes || !json.Valid(checkpoint.Body) {
		return nil, errors.New("invalid native compaction source checkpoint")
	}
	if _, err := buildLocalCompactionRequest(checkpoint.Body); err != nil {
		return nil, err
	}
	return &remoteCompactionSource{body: checkpoint.Body, headers: make(http.Header)}, nil
}

type nativeCompactionSourceMiddleware struct {
	pipeline.DummyMiddleware

	outbound *PersistentOutboundTransformer
	adapter  *remoteCompactionAdapter
}

func retainNativeCompactionSources(outbound *PersistentOutboundTransformer, adapter *remoteCompactionAdapter) pipeline.Middleware {
	return &nativeCompactionSourceMiddleware{outbound: outbound, adapter: adapter}
}

func (m *nativeCompactionSourceMiddleware) Name() string { return "retain-native-compaction-source" }

func (m *nativeCompactionSourceMiddleware) snapshot() (*remoteCompactionSource, *PersistenceState) {
	if m.outbound == nil || m.outbound.state == nil || m.adapter == nil || m.adapter.systemService == nil {
		return nil, nil
	}
	state := m.outbound.state
	raw := state.RawProviderRequest
	if raw == nil || state.APIKey == nil || state.APIKey.ID <= 0 || state.APIKey.ProjectID <= 0 ||
		!isResponsesReasoningRecoveryFormat(raw.APIFormat) {
		return nil, nil
	}
	body := raw.Body
	if raw.APIFormat == string(llm.APIFormatOpenAIResponseCompact) {
		envelope, input, err := decodeResponsesInput(body)
		if err != nil {
			return nil, nil
		}
		input = append(input, json.RawMessage(`{"type":"compaction_trigger"}`))
		if setResponseEnvelopeInput(envelope, input) != nil {
			return nil, nil
		}
		body, err = json.Marshal(envelope)
		if err != nil {
			return nil, nil
		}
	} else if !bytes.Contains(body, []byte(`"compaction_trigger"`)) {
		return nil, nil
	}
	if _, err := buildLocalCompactionRequest(body); err != nil {
		return nil, nil
	}
	owner := &ent.APIKey{ID: state.APIKey.ID, ProjectID: state.APIKey.ProjectID}
	return &remoteCompactionSource{body: append([]byte(nil), body...)}, &PersistenceState{APIKey: owner}
}

func (m *nativeCompactionSourceMiddleware) OnOutboundRawResponse(ctx context.Context, response *httpclient.Response) (*httpclient.Response, error) {
	source, state := m.snapshot()
	if source == nil || response == nil || response.StatusCode >= 400 {
		return response, nil
	}
	if status := gjson.GetBytes(response.Body, "status").String(); status != "" && status != "completed" {
		return response, nil
	}
	for _, item := range gjson.GetBytes(response.Body, "output").Array() {
		if err := m.saveItem(ctx, source, state, item); err != nil {
			return nil, err
		}
	}
	return response, nil
}

func (m *nativeCompactionSourceMiddleware) saveItem(ctx context.Context, source *remoteCompactionSource, state *PersistenceState, item gjson.Result) error {
	if item.Get("type").String() != remoteCompactionItemType && item.Get("type").String() != legacyRemoteCompactionSummaryType {
		return nil
	}
	ref := &remoteCompactionReference{ID: item.Get("id").String(), EncryptedContent: item.Get("encrypted_content").String()}
	if ref.ID == "" || ref.EncryptedContent == "" || isLocalCompactionReference(ref) {
		return nil
	}
	return m.adapter.retainNativeCompactionSource(ctx, ref, state, source)
}

func (m *nativeCompactionSourceMiddleware) OnOutboundRawStream(ctx context.Context, stream streams.Stream[*httpclient.StreamEvent]) (streams.Stream[*httpclient.StreamEvent], error) {
	source, state := m.snapshot()
	if source == nil {
		return stream, nil
	}
	return &nativeCompactionSourceStream{Stream: stream, ctx: ctx, middleware: m, source: source, state: state, saved: make(map[string]bool)}, nil
}

type nativeCompactionSourceStream struct {
	streams.Stream[*httpclient.StreamEvent]

	ctx        context.Context
	middleware *nativeCompactionSourceMiddleware
	source     *remoteCompactionSource
	state      *PersistenceState
	saved      map[string]bool
	current    *httpclient.StreamEvent
	err        error
}

func (s *nativeCompactionSourceStream) Next() bool {
	if s.err != nil || !s.Stream.Next() {
		return false
	}
	event := s.Stream.Current()
	s.current = event
	if event == nil {
		return true
	}
	kind := gjson.GetBytes(event.Data, "type").String()
	var items []gjson.Result
	switch kind {
	case "response.output_item.done":
		items = append(items, gjson.GetBytes(event.Data, "item"))
	case "response.completed":
		items = gjson.GetBytes(event.Data, "response.output").Array()
	}
	for _, item := range items {
		if item.Get("type").String() != remoteCompactionItemType && item.Get("type").String() != legacyRemoteCompactionSummaryType {
			continue
		}
		key := remoteCompactionCacheKey(&remoteCompactionReference{ID: item.Get("id").String(), EncryptedContent: item.Get("encrypted_content").String()})
		if s.saved[key] {
			continue
		}
		if err := s.middleware.saveItem(s.ctx, s.source, s.state, item); err != nil {
			s.err = fmt.Errorf("retain native compaction source before publishing checkpoint: %w", err)
			_ = s.Stream.Close()
			return false
		}
		s.saved[key] = true
	}
	return true
}

func (s *nativeCompactionSourceStream) Current() *httpclient.StreamEvent {
	return s.current
}

func (s *nativeCompactionSourceStream) Err() error {
	if s.err != nil {
		return s.err
	}
	return s.Stream.Err()
}

// Summary checkpoints use the existing owner-bound encryption and remain
// readable after request logs or the original native source are removed.
func (a *remoteCompactionAdapter) retainCompactionSummary(ctx context.Context, cacheKey string, ref *remoteCompactionReference, state *PersistenceState, summary string) error {
	if a.systemService == nil {
		return nil
	}
	return a.retainLegacyCompactionSummary(ctx, cacheKey, ref, state, summary)
}
