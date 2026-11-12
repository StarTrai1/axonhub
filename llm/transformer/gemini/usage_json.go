package gemini

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/looplj/axonhub/llm/internal/pkg/xjson"
)

// Accept quoted counters from compatible ProtoJSON endpoints without changing
// numeric output, cumulative stream snapshots, or optional cost parsing.
func (u *UsageMetadata) UnmarshalJSON(data []byte) error {
	normalized, err := normalizeGeminiTokenCounts(data,
		"promptTokenCount", "toolUsePromptTokenCount", "candidatesTokenCount",
		"totalTokenCount", "cachedContentTokenCount", "thoughtsTokenCount")
	if err != nil {
		return err
	}
	type alias UsageMetadata
	aux := struct {
		*alias

		Cost json.RawMessage `json:"cost"`
	}{alias: (*alias)(u)}
	if err := json.Unmarshal(normalized, &aux); err != nil {
		return err
	}

	u.Cost = xjson.ParseOptionalFloat64(aux.Cost)
	return nil
}

// unmarshalStreamResponse merges cumulative usage only for streaming responses.
// Decode into a copy so invalid chunks cannot mutate the previous snapshot.
func unmarshalStreamResponse(data []byte, previous *UsageMetadata) (GenerateContentResponse, error) {
	var response GenerateContentResponse
	if previous != nil {
		usage := *previous
		// Decode details into fresh storage; JSON decoding reuses slice elements.
		usage.PromptTokensDetails = nil
		usage.CandidatesTokensDetails = nil
		response.UsageMetadata = &usage
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return GenerateContentResponse{}, err
	}
	if previous != nil && response.UsageMetadata != nil {
		response.UsageMetadata.PromptTokensDetails = mergeModalitySnapshots(previous.PromptTokensDetails, response.UsageMetadata.PromptTokensDetails)
		response.UsageMetadata.CandidatesTokensDetails = mergeModalitySnapshots(previous.CandidatesTokensDetails, response.UsageMetadata.CandidatesTokensDetails)
	}
	if previous != nil && previous.Cost != nil && response.UsageMetadata != nil {
		var fields struct {
			UsageMetadata struct {
				Cost json.RawMessage `json:"cost"`
			} `json:"usageMetadata"`
		}
		if err := json.Unmarshal(data, &fields); err != nil {
			return GenerateContentResponse{}, err
		}
		if len(fields.UsageMetadata.Cost) == 0 {
			response.UsageMetadata.Cost = previous.Cost
		}
	}
	return response, nil
}

// Each reported modality replaces its previous cumulative snapshot, including
// explicit zero. Omitted modalities survive; duplicates within one frame remain
// separate for settlement. Never mutate an earlier frame's detail pointers.
func mergeModalitySnapshots(previous, incoming []*ModalityTokenCount) []*ModalityTokenCount {
	if len(previous) == 0 && len(incoming) == 0 {
		return nil
	}
	reported := make(map[string]bool, len(incoming))
	for _, detail := range incoming {
		if detail != nil {
			reported[strings.ToUpper(strings.TrimSpace(detail.Modality))] = true
		}
	}
	merged := make([]*ModalityTokenCount, 0, len(previous)+len(incoming))
	for _, detail := range previous {
		if detail != nil && !reported[strings.ToUpper(strings.TrimSpace(detail.Modality))] {
			copy := *detail
			merged = append(merged, &copy)
		}
	}
	for _, detail := range incoming {
		if detail != nil {
			copy := *detail
			merged = append(merged, &copy)
		}
	}
	return merged
}

func (m *ModalityTokenCount) UnmarshalJSON(data []byte) error {
	normalized, err := normalizeGeminiTokenCounts(data, "tokenCount")
	if err != nil {
		return err
	}
	type wire ModalityTokenCount
	var value wire
	if err := json.Unmarshal(normalized, &value); err != nil {
		return err
	}
	*m = ModalityTokenCount(value)
	return nil
}

func normalizeGeminiTokenCounts(data []byte, fields ...string) ([]byte, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return nil, err
	}
	changed := false
	for _, field := range fields {
		raw := object[field]
		if len(raw) == 0 || raw[0] != '"' {
			continue
		}
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return nil, fmt.Errorf("invalid Gemini %s", field)
		}
		count, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
		if err != nil || count < 0 {
			return nil, fmt.Errorf("invalid Gemini %s", field)
		}
		object[field] = json.RawMessage(strconv.FormatInt(count, 10))
		changed = true
	}
	if !changed {
		return data, nil
	}
	return json.Marshal(object)
}
