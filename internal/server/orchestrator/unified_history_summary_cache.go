package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strings"
)

// Canonicalize objects only. Arrays, strings (including tool arguments), and
// JSON numbers retain their original meaning and order.
func canonicalHistoryJSON(raw []byte) ([]byte, error) {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func setUnifiedSummaryKeys(segment *unifiedHistorySegment, contract map[string]json.RawMessage) error {
	raw, err := json.Marshal(contract)
	if err != nil {
		return err
	}
	canonical, err := canonicalHistoryJSON(raw)
	if err != nil {
		return err
	}
	key := func(body []byte) string {
		return "summary:" + historyDigest(append([]byte("segment-summary-v2:"), body...))
	}
	segment.key = key(canonical)
	segment.legacyAlias = "summaryref:" + strings.TrimPrefix(segment.key, "summary:")
	semantic, err := unifiedSummaryContract(contract, true)
	if err != nil { return err }
	segment.cacheAlias = "summaryref:" + historyDigest(append([]byte("segment-facts-v3:"), semantic...))
	segment.legacyKeys = []string{key(raw)}
	// Agent transport used to reserialize custom calls but leave their outputs
	// and ordinary messages untouched. Sorting every item would miss those
	// already completed summaries too. Read each historical representation.
	var input []json.RawMessage
	if err := json.Unmarshal(contract["input"], &input); err != nil {
		return err
	}
	for _, types := range [][]string{{"custom_tool_call"}, {"custom_tool_call", "function_call"}, nil} {
		items := slices.Clone(input)
		for i, raw := range input {
			if types != nil && !slices.Contains(types, rawInputItemType(raw)) {
				continue
			}
			var item map[string]json.RawMessage
			if err := json.Unmarshal(raw, &item); err != nil {
				return err
			}
			items[i], err = json.Marshal(item)
			if err != nil {
				return err
			}
		}
		legacy := maps.Clone(contract)
		legacy["input"], err = json.Marshal(items)
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(legacy)
		if err != nil {
			return err
		}
		candidate := key(encoded)
		if !slices.Contains(segment.legacyKeys, candidate) {
			segment.legacyKeys = append(segment.legacyKeys, candidate)
		}
	}
	if !slices.Contains(segment.legacyKeys, segment.key) {
		segment.legacyKeys = append(segment.legacyKeys, segment.key)
	}
	// Import completed factual notes produced at a stronger effort. This only
	// adds an exact old contract lookup; it never changes today's request.
	var reasoning map[string]json.RawMessage
	if json.Unmarshal(contract["reasoning"], &reasoning) == nil && len(reasoning) > 0 {
		for _, effort := range []string{"xhigh", "high"} {
			prior := maps.Clone(contract)
			r := maps.Clone(reasoning)
			r["effort"], _ = json.Marshal(effort)
			prior["reasoning"], _ = json.Marshal(r)
			raw, err := json.Marshal(prior)
			if err != nil { return err }
			canonical, err := canonicalHistoryJSON(raw)
			if err != nil { return err }
			old := key(canonical)
			if !slices.Contains(segment.legacyKeys, old) { segment.legacyKeys = append(segment.legacyKeys, old) }
		}
	}
	return nil
}

func unifiedSummaryLookupKeys(segment *unifiedHistorySegment) []string {
	keys := append([]string{segment.key}, segment.legacyKeys...)
	if segment.cacheAlias != "" {
		keys = append(keys, segment.cacheAlias)
	}
	if segment.legacyAlias != "" { keys = append(keys, segment.legacyAlias) }
	return keys
}

type unifiedSummaryValue struct {
	key, text string
}

func (a *remoteCompactionAdapter) loadUnifiedSummary(ctx context.Context, segment *unifiedHistorySegment, entries map[string]string) (unifiedSummaryValue, bool, error) {
	keys := segment.legacyKeys
	if len(keys) == 0 {
		keys = []string{segment.key}
	}
	aliasKey := segment.cacheAlias
	if entries[aliasKey] == "" && entries[segment.legacyAlias] != "" { aliasKey = segment.legacyAlias }
	if sealed := entries[aliasKey]; sealed != "" {
		alias, err := a.openHistoryRecord(ctx, aliasKey, sealed, 128)
		if err != nil {
			return unifiedSummaryValue{}, false, err
		}
		target := string(alias)
		if !strings.HasPrefix(target, "summary:") || len(target) != len("summary:")+64 {
			return unifiedSummaryValue{}, false, errors.New("invalid unified summary alias")
		}
		keys = []string{target}
		if entries[target] == "" {
			loaded, err := a.systemService.LoadUnifiedHistory(ctx, keys)
			if err != nil {
				return unifiedSummaryValue{}, false, err
			}
			entries = loaded
		}
		if entries[target] == "" {
			return unifiedSummaryValue{}, false, errors.New("unified summary alias target is unavailable")
		}
	}
	for _, key := range keys {
		if sealed := entries[key]; sealed != "" {
			text, err := a.openHistoryRecord(ctx, key, sealed, localCompactionMaxSummaryBytes)
			return unifiedSummaryValue{key: key, text: string(text)}, err == nil, err
		}
	}
	return unifiedSummaryValue{}, false, nil
}

// Preserve the winning old source label as well as its text. Merely changing
// every label on upgrade would invalidate otherwise reusable prompt prefixes.
func (a *remoteCompactionAdapter) publishUnifiedSummaryAlias(ctx context.Context, segment *unifiedHistorySegment, value unifiedSummaryValue) (unifiedSummaryValue, error) {
	if segment.cacheAlias == "" {
		return value, nil
	}
	sealed, err := a.sealHistoryRecord(ctx, segment.cacheAlias, []byte(value.key))
	if err != nil {
		return unifiedSummaryValue{}, err
	}
	if err := a.systemService.SaveUnifiedHistory(ctx, map[string]string{segment.cacheAlias: sealed}); err != nil {
		return unifiedSummaryValue{}, err
	}
	entries, err := a.systemService.LoadUnifiedHistory(ctx, []string{segment.cacheAlias, value.key})
	if err != nil {
		return unifiedSummaryValue{}, err
	}
	published, found, err := a.loadUnifiedSummary(ctx, segment, entries)
	if err != nil {
		return unifiedSummaryValue{}, err
	}
	if !found {
		return unifiedSummaryValue{}, errors.New("unified summary alias target is unavailable")
	}
	return published, nil
}
