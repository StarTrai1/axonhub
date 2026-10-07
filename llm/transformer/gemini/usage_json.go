package gemini

import (
	"encoding/json"
	"strings"

	"github.com/looplj/axonhub/llm/internal/pkg/xjson"
)

func (u *UsageMetadata) UnmarshalJSON(data []byte) error {
	type alias UsageMetadata
	aux := struct {
		*alias

		Cost json.RawMessage `json:"cost"`
	}{alias: (*alias)(u)}
	if err := json.Unmarshal(data, &aux); err != nil {
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
