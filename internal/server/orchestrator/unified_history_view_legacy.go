package orchestrator

import (
	"bytes"
	"context"
	"strings"
	"time"

	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

func (m *responsesCompactionRecoveryMiddleware) importUnifiedView(ctx context.Context, destination *httpclient.Request, key responsesCompactionRecoveryKey, keys []string) ([]byte, int, error) {
	service := m.adapter.requestService
	channel := m.outbound.GetCurrentChannel()
	if service == nil || channel == nil {
		return nil, 0, nil
	}
	account := destination.Headers.Get("Chatgpt-Account-Id")
	suffix, suffixOK := responsesHistoryCredentialSuffix(ctx, channel, destination)
	if channel.Credentials.IsOAuth() {
		if account == "" {
			return nil, 0, nil
		}
	} else if !suffixOK {
		return nil, 0, nil
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, err := service.FindUnifiedViewCandidates(lookupCtx, channel.ID, channel.UpdatedAt)
	if err != nil {
		return nil, 0, err
	}
	loadedBytes := 0
	for _, row := range rows {
		if row.RequestURL != destination.URL || row.Edges.Request == nil {
			continue
		}
		headers := decodeStoredHeaders(row.RequestHeaders)
		if channel.Credentials.IsOAuth() {
			if headers.Get("Chatgpt-Account-Id") != account {
				continue
			}
		} else if row.ChannelAPIKeySuffix == nil || *row.ChannelAPIKeySuffix != suffix {
			continue
		}
		prior := shared.ReadCodexRequestMetadata(headers, nil)
		if prior.ThreadID != key.scope.threadID || prior.WindowID != key.scope.windowID {
			continue
		}
		original, err := service.LoadRequestBody(lookupCtx, row.Edges.Request)
		if err != nil {
			return nil, 0, err
		}
		loadedBytes += len(original)
		if loadedBytes > 32<<20 {
			break
		}
		if gjson.GetBytes(original, "previous_response_id").String() != "" || gjson.GetBytes(original, `input.#(type=="compaction_trigger")`).Exists() {
			continue
		}
		oldKeys, err := unifiedViewKeys(key, original)
		if err != nil {
			return nil, 0, err
		}
		covered := len(oldKeys)
		if covered == 0 || covered > len(keys) || oldKeys[covered-1] != keys[covered-1] {
			continue
		}
		view, err := service.LoadRequestExecutionRequestBody(lookupCtx, row)
		if err != nil {
			return nil, 0, err
		}
		loadedBytes += len(view)
		if loadedBytes > 32<<20 {
			break
		}
		if gjson.GetBytes(view, "previous_response_id").String() != "" ||
			gjson.GetBytes(view, "model").String() != key.scope.provider.model ||
			!bytes.Contains(view, []byte("Archived history summary (source ")) {
			continue
		}
		// Logs are only imported when every summary is an authenticated existing
		// result. User text containing a lookalike marker is not a compiled view.
		if !m.adapter.verifiedUnifiedViewSummaries(lookupCtx, view) {
			continue
		}
		if err := m.adapter.saveHistorySnapshot(lookupCtx, keys[covered-1], view, nil); err != nil {
			return nil, 0, err
		}
		log.Info(ctx, "restored accepted unified history view from completed request", log.Int("request_id", row.RequestID), log.Int("covered_items", covered))
		return view, covered, nil
	}
	return nil, 0, nil
}

func (a *remoteCompactionAdapter) verifiedUnifiedViewSummaries(ctx context.Context, body []byte) bool {
	const prefix = "Archived history summary (source "
	const separator = "). This is a derived summary; the original user instructions remain authoritative.\n"
	count := 0
	for _, item := range gjson.GetBytes(body, "input").Array() {
		if item.Get("role").String() != "assistant" {
			continue
		}
		for _, part := range item.Get("content").Array() {
			text := part.Get("text").String()
			if !strings.HasPrefix(text, prefix) {
				continue
			}
			digest, summary, found := strings.Cut(strings.TrimPrefix(text, prefix), separator)
			if !found || len(digest) != 64 {
				return false
			}
			key := "summary:" + digest
			entries, err := a.systemService.LoadUnifiedHistory(ctx, []string{key})
			if err != nil || entries[key] == "" {
				return false
			}
			raw, err := a.openHistoryRecord(ctx, key, entries[key], localCompactionMaxSummaryBytes)
			if err != nil || string(raw) != summary {
				return false
			}
			count++
		}
	}
	return count > 0
}
