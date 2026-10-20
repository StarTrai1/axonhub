package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
)

// A successful native read is evidence for a specific provider/account/model,
// never permission to route outside the request's allowed candidates.
func historyNativeOriginKey(ref *remoteCompactionReference, channelID int) string {
	return "origin:" + historyDigest([]byte(remoteCompactionCacheKey(ref)+historyWireAlias("native", channelID)))
}

func (a *remoteCompactionAdapter) rememberNativeHistoryOrigins(ctx context.Context, body, response []byte, reader nativeHistoryReader) error {
	origin := reader.Provider
	// Standalone compact output is canonical but does not certify that a
	// different /responses endpoint can read it. Wait for a completed read.
	if origin.Format == string(llm.APIFormatOpenAIResponseCompact) {
		return nil
	}
	if origin.ChannelID <= 0 || origin.Auth == "" || origin.Endpoint == "" || origin.Model == "" {
		return nil
	}
	encoded, err := json.Marshal(reader)
	if err != nil {
		return err
	}
	entries := make(map[string]string)
	for _, items := range []gjson.Result{gjson.GetBytes(body, "input"), gjson.GetBytes(response, "output")} {
		for _, item := range items.Array() {
			if item.Get("type").String() != remoteCompactionItemType && item.Get("type").String() != legacyRemoteCompactionSummaryType {
				continue
			}
			ref := &remoteCompactionReference{ID: item.Get("id").String(), EncryptedContent: item.Get("encrypted_content").String()}
			if ref.ID == "" || ref.EncryptedContent == "" || isLocalCompactionReference(ref) {
				continue
			}
			key := historyNativeOriginKey(ref, origin.ChannelID)
			entries[key], err = a.sealHistoryRecord(ctx, key, encoded)
			if err != nil {
				return err
			}
		}
	}
	return a.systemService.SaveUnifiedHistory(ctx, entries)
}

func (a *remoteCompactionAdapter) loadNativeHistoryReader(ctx context.Context, ref *remoteCompactionReference, candidate *ChannelModelsCandidate) (*nativeHistoryReader, error) {
	if !a.unifiedHistoryEnabled() || ref == nil || candidate == nil || candidate.Channel == nil {
		return nil, nil
	}
	key := historyNativeOriginKey(ref, candidate.Channel.ID)
	entries, err := a.systemService.LoadUnifiedHistory(ctx, []string{key})
	if err != nil || entries[key] == "" {
		return nil, err
	}
	raw, err := a.openHistoryRecord(ctx, key, entries[key], 8192)
	if err != nil {
		return nil, err
	}
	var reader nativeHistoryReader
	if json.Unmarshal(raw, &reader) != nil || reader.Provider.ChannelID != candidate.Channel.ID || reader.Provider.Auth == "" {
		return nil, errors.New("invalid checkpoint reader provenance")
	}
	return &reader, nil
}

func historyExportMatches(reader nativeHistoryReader, candidate *ChannelModelsCandidate, request *httpclient.Request) bool {
	origin := reader.Provider
	if reader.OAuth != candidate.Channel.Credentials.IsOAuth() {
		return false
	}
	endpoint := func(value string) string {
		return strings.Replace(strings.Replace(value, "wss://", "https://", 1), "ws://", "http://", 1)
	}
	return origin.ChannelID == candidate.Channel.ID && origin.Model == gjson.GetBytes(request.Body, "model").String() &&
		origin.Auth != "" && origin.Auth == historyReaderCredential(request, reader.OAuth) && endpoint(origin.Endpoint) == endpoint(request.URL)
}

// Export only the checkpoint window, once, on a proven reader. The exact live
// suffix is never summarized. This is a portable handoff, not decrypted or
// lossless access to hidden reasoning. Full raw archives remain independent.
func (a *remoteCompactionAdapter) exportUnifiedCheckpoint(ctx context.Context, body []byte, state *PersistenceState, executor pipeline.Executor) ([]byte, bool, error) {
	ref, _, _, err := parseRemoteCompactionRequest(body)
	if err != nil || ref == nil || isLocalCompactionReference(ref) || gjson.GetBytes(body, "previous_response_id").String() != "" {
		return body, false, err
	}
	envelope, input, err := decodeResponsesInput(body)
	if err != nil {
		return nil, false, err
	}
	count := 0
	for _, item := range input {
		if rawInputItemType(item) == remoteCompactionItemType || rawInputItemType(item) == legacyRemoteCompactionSummaryType {
			count++
		}
	}
	if count != 1 {
		return body, false, nil
	}
	prefix := append(append([]json.RawMessage(nil), input[:ref.Index+1]...), json.RawMessage(`{"type":"compaction_trigger"}`))
	if err := setResponseEnvelopeInput(envelope, prefix); err != nil {
		return nil, false, err
	}
	contract, err := unifiedSummaryContract(envelope, true)
	if err != nil {
		return nil, false, err
	}
	key := "native:" + historyDigest(append([]byte("checkpoint-context-export-v1:"), contract...))
	load := func(ctx context.Context) (string, error) {
		entries, err := a.systemService.LoadUnifiedHistory(ctx, []string{key})
		if err != nil || entries[key] == "" {
			return "", err
		}
		text, err := a.openHistoryRecord(ctx, key, entries[key], localCompactionMaxSummaryBytes)
		return string(text), err
	}
	summary, err := load(ctx)
	if err != nil {
		return nil, false, err
	}
	if summary == "" {
		candidates := state.unifiedHistoryCandidates
		if len(candidates) == 0 {
			candidates = state.ChannelModelsCandidates
		}
		var selected *ChannelModelsCandidate
		var reader nativeHistoryReader
		for _, candidate := range candidates {
			if candidate == nil || candidate.Channel == nil {
				continue
			}
			originKey := historyNativeOriginKey(ref, candidate.Channel.ID)
			entries, err := a.systemService.LoadUnifiedHistory(ctx, []string{originKey})
			if err != nil {
				return nil, false, err
			}
			if entries[originKey] == "" {
				continue
			}
			raw, err := a.openHistoryRecord(ctx, originKey, entries[originKey], 8192)
			if err != nil {
				return nil, false, err
			}
			if json.Unmarshal(raw, &reader) != nil || reader.Provider.ChannelID != candidate.Channel.ID || reader.Provider.Auth == "" {
				return nil, false, errors.New("invalid checkpoint reader provenance")
			}
			if rejected := state.unifiedRejectedReader; rejected != nil && rejected.Provider.ChannelID == reader.Provider.ChannelID && rejected.Provider.Auth == reader.Provider.Auth && rejected.Provider.Model == reader.Provider.Model {
				continue
			}
			for i, model := range candidate.Models {
				if model.ActualModel != reader.Provider.Model {
					continue
				}
				clone := *candidate
				clone.Models = candidate.Models[i : i+1]
				clone.APIFormat = string(llm.APIFormatOpenAIResponse)
				selected = &clone
				break
			}
			if selected != nil {
				break
			}
		}
		if selected == nil {
			return body, false, nil
		}
		source, err := json.Marshal(envelope)
		if err != nil {
			return nil, false, err
		}
		source, err = protectUnifiedHistory(ctx, source, state)
		if err != nil {
			return nil, false, err
		}
		// The source reader must receive only a bounded existing native window.
		if unifiedContextSize(source) > unifiedContextMaxBytes {
			return body, false, nil
		}
		exportState := *state
		exportState.CurrentCandidate, exportState.CurrentModelIndex = selected, 0
		exportState.ChannelModelsCandidates = []*ChannelModelsCandidate{selected}
		exportState.RetryPolicyProvider = nil
		exportState.unifiedExportOrigin = &reader
		exportState.compactionRecovery = &compactionRecoveryEvidence{}
		failureKey := unifiedSegmentFailureKey(&exportState, &unifiedHistorySegment{key: key})
		if err := a.unifiedSummaryCooldown(failureKey, time.Now()); err != nil {
			return nil, false, err
		}
		result, err := waitForUnifiedHistoryFlight(ctx, &a.summaryGenerator, remoteCompactionOwnerCacheKey(state, key), func() (any, error) {
			if value, err := load(ctx); err != nil || value != "" {
				return value, err
			}
			exportCtx, cancel := context.WithTimeout(contexts.CloneRequestContext(ctx), 90*time.Second)
			defer cancel()
			value, err := a.generateLocalSummary(exportCtx, key, &remoteCompactionSource{body: source, headers: make(http.Header)}, &exportState, executor)
			if err != nil {
				a.rememberUnifiedSummaryFailure(ctx, failureKey, err)
				return nil, err
			}
			persistCtx, persistCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer persistCancel()
			sealed, err := a.sealHistoryRecord(persistCtx, key, []byte(value))
			if err != nil {
				return nil, err
			}
			if err := a.systemService.SaveUnifiedHistory(persistCtx, map[string]string{key: sealed}); err != nil {
				return nil, err
			}
			return load(persistCtx)
		})
		if err != nil {
			return nil, false, err
		}
		var ok bool
		summary, ok = result.(string)
		if !ok || strings.TrimSpace(summary) == "" {
			return nil, false, errors.New("empty checkpoint context export")
		}
		log.Info(ctx, "exported portable context from verified native checkpoint reader", log.Int("source_channel_id", selected.Channel.ID))
	}
	result, err := replaceRemoteCompactionWithLocalSummary(body, summary)
	if err != nil {
		return nil, false, err
	}
	result, err = protectUnifiedHistory(ctx, result, state)
	if err != nil {
		return nil, false, err
	}
	if unifiedContextSize(result) > unifiedContextMaxBytes {
		return nil, false, unifiedHistoryPreparationLimit("exported checkpoint and exact live suffix exceed the input budget")
	}
	return result, true, nil
}
