package gemini

import (
	"encoding/json"

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
		response.UsageMetadata = &usage
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return GenerateContentResponse{}, err
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
