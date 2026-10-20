package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"strings"
)

// Coverage is immutable evidence of the original items a completed summary
// describes. It is not a mutable "latest conversation" pointer. New runtime
// instructions may precede a range without changing that range's boundaries.
type unifiedSummaryCoverage struct {
	Version int      `json:"version"`
	Items   []string `json:"items"`
	Summary string   `json:"summary"`
}

func unifiedSummaryContract(contract map[string]json.RawMessage, input bool) ([]byte, error) {
	semantic := make(map[string]json.RawMessage)
	for _, field := range []string{"model", "destination_model", "instructions", "tools"} {
		if value := contract[field]; len(value) > 0 {
			if field == "instructions" && (string(value) == "null" || string(value) == `""`) {
				continue
			}
			semantic[field] = value
		}
	}
	if input {
		semantic["input"] = contract["input"]
	}
	raw, err := json.Marshal(semantic)
	if err != nil {
		return nil, err
	}
	return canonicalHistoryJSON(raw)
}

func unifiedCoverageItem(item json.RawMessage) (string, error) {
	canonical, err := unifiedViewItemIdentity(item)
	if err != nil {
		return "", err
	}
	return historyDigest([]byte(canonical)), nil
}

func unifiedCoverageKey(contract []byte, first string) string {
	return "coverage:" + historyDigest(append(append([]byte("original-segment-coverage-v1:"), contract...), []byte(first)...))
}

func setUnifiedCoverageIdentity(segment *unifiedHistorySegment, contract map[string]json.RawMessage) error {
	if len(segment.items) == 0 {
		return nil
	}
	header, err := unifiedSummaryContract(contract, false)
	if err != nil {
		return err
	}
	segment.coverageItems = make([]string, len(segment.items))
	for i, item := range segment.items {
		segment.coverageItems[i], err = unifiedCoverageItem(item)
		if err != nil {
			return err
		}
	}
	segment.coverageKey = unifiedCoverageKey(header, segment.coverageItems[0])
	return nil
}

func (a *remoteCompactionAdapter) publishUnifiedCoverage(ctx context.Context, segment *unifiedHistorySegment) error {
	if segment.coverageKey == "" || segment.summary == "" || len(segment.coverageItems) == 0 {
		return nil
	}
	value, err := json.Marshal(unifiedSummaryCoverage{Version: 1, Items: segment.coverageItems, Summary: segment.key})
	if err != nil {
		return err
	}
	sealed, err := a.sealHistoryRecord(ctx, segment.coverageKey, value)
	if err != nil {
		return err
	}
	return a.systemService.SaveUnifiedHistory(ctx, map[string]string{segment.coverageKey: sealed})
}

func (a *remoteCompactionAdapter) loadUnifiedCoverage(ctx context.Context, envelope map[string]json.RawMessage, items []json.RawMessage, state *PersistenceState, candidate *ChannelModelsCandidate) (map[int]unifiedHistorySegment, error) {
	contract := maps.Clone(envelope)
	if state.CurrentModelIndex >= 0 && state.CurrentModelIndex < len(candidate.Models) {
		contract["destination_model"], _ = json.Marshal(candidate.Models[state.CurrentModelIndex].ActualModel)
	}
	header, err := unifiedSummaryContract(contract, false)
	if err != nil {
		return nil, err
	}
	units := unifiedHistoryUnits(items)
	if len(units) < 2 {
		return nil, nil
	}
	identities := make([]string, len(items))
	for i, item := range items {
		identities[i], err = unifiedCoverageItem(item)
		if err != nil {
			return nil, err
		}
	}
	positions := make(map[string][]int)
	boundaries := make(map[int]bool)
	keys := make([]string, 0, len(units))
	position := 0
	for _, unit := range units[:len(units)-1] {
		key := unifiedCoverageKey(header, identities[position])
		if len(positions[key]) == 0 {
			keys = append(keys, key)
		}
		positions[key] = append(positions[key], position)
		position += len(unit)
		boundaries[position] = true
	}
	entries, err := a.systemService.LoadUnifiedHistory(ctx, keys)
	if err != nil {
		return nil, err
	}
	matched := make(map[int]unifiedHistorySegment)
	for _, key := range keys {
		if entries[key] == "" {
			continue
		}
		raw, err := a.openHistoryRecord(ctx, key, entries[key], 4<<20)
		if err != nil {
			return nil, err
		}
		var coverage unifiedSummaryCoverage
		if json.Unmarshal(raw, &coverage) != nil || coverage.Version != 1 || len(coverage.Items) == 0 ||
			len(coverage.Items) > 65536 || !strings.HasPrefix(coverage.Summary, "summary:") || len(coverage.Summary) != 72 {
			return nil, errors.New("invalid unified summary coverage")
		}
		for _, start := range positions[key] {
			end := start + len(coverage.Items)
			if end > position || !boundaries[end] {
				continue
			}
			same := true
			for i, identity := range coverage.Items {
				if identities[start+i] != identity {
					same = false
					break
				}
			}
			if !same {
				continue
			}
			stored, err := a.systemService.LoadUnifiedHistory(ctx, []string{coverage.Summary})
			if err != nil {
				return nil, err
			}
			if stored[coverage.Summary] == "" {
				return nil, errors.New("unified coverage summary is unavailable")
			}
			text, err := a.openHistoryRecord(ctx, coverage.Summary, stored[coverage.Summary], localCompactionMaxSummaryBytes)
			if err != nil {
				return nil, err
			}
			matched[start] = unifiedHistorySegment{items: items[start:end], key: coverage.Summary, summary: string(text)}
		}
	}
	return matched, nil
}

// Upgrade old completed summaries using the checkpoint's original source and
// original contract, before today's inserted tools can shift any boundaries.
// This function performs no generation and never imports an unverified label.
func (a *remoteCompactionAdapter) importUnifiedSourceCoverage(ctx context.Context, body []byte, state *PersistenceState, candidate *ChannelModelsCandidate, budget int) error {
	ref, _, _, err := parseRemoteCompactionRequest(body)
	if err != nil || ref == nil {
		return err
	}
	source, err := a.loadNativeCompactionSource(ctx, ref, state)
	if err != nil || source == nil {
		return err
	}
	expanded, complete, err := a.expandUnifiedHistory(ctx, source.body, state, make(map[string]bool))
	if err != nil || !complete {
		return err
	}
	expanded, err = protectUnifiedHistory(ctx, expanded, state)
	if err != nil {
		return err
	}
	envelope, items, err := decodeResponsesInput(expanded)
	if err != nil {
		return err
	}
	items, _, portable := unifiedPortableItems(items)
	if !portable {
		return nil
	}
	plan, supported, err := planUnifiedHistory(envelope, items, state, candidate, budget)
	if err != nil || !supported {
		return err
	}
	if _, err := a.loadUnifiedSegments(ctx, plan.segments); err != nil {
		return err
	}
	for i := range plan.segments {
		if err := a.publishUnifiedCoverage(ctx, &plan.segments[i]); err != nil {
			return err
		}
	}
	return nil
}
