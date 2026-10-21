package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
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
	if len(entries) == 0 {
		return nil
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
