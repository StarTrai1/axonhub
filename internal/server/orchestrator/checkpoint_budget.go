package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
)

// Admission belongs to the completed provider request, after model selection,
// tool conversion, prompt injection and operator overrides. Source archives do
// not describe the model or the complete input of this new summary request.
func admitCheckpointSummary(outbound *PersistentOutboundTransformer) pipeline.Middleware {
	return pipeline.OnRawRequest("admit-checkpoint-summary", func(_ context.Context, request *httpclient.Request) (*httpclient.Request, error) {
		if outbound == nil || outbound.state == nil || !outbound.state.checkpointSummaryGeneration || request == nil {
			return request, nil
		}
		if err := validateCheckpointSummaryRequest(request.Body, outbound.state); err != nil {
			return nil, err
		}
		return request, nil
	})
}

func validateCheckpointSummaryRequest(body []byte, state *PersistenceState) error {
	candidate := state.CurrentCandidate
	if candidate == nil || state.CurrentModelIndex < 0 || state.CurrentModelIndex >= len(candidate.Models) {
		return checkpointSummaryError("compaction_model_unavailable", "Checkpoint summary has no selected provider model")
	}
	model := gjson.GetBytes(body, "model").String()
	contextTokens := candidate.ContextTokens
	if model != candidate.Models[state.CurrentModelIndex].ActualModel {
		// A final body override may change the model. A window configured for the
		// original route cannot authorize input for that different model.
		contextTokens = 0
	}
	if contextTokens <= 0 {
		contextTokens = biz.BundledModelContextLimit(model)
	}
	if contextTokens <= 0 {
		return nil // Unknown model contracts retain provider-side admission.
	}
	selected := *candidate
	selected.Models = []biz.ChannelModelEntry{{ActualModel: model}}
	_, budget := unifiedHistoryInputBudgets(body, &selected, 0, contextTokens)
	estimated, err := checkpointContextSize(body)
	if err != nil {
		return checkpointSummaryError("compaction_source_invalid", "Checkpoint summary input is not a valid JSON request")
	}
	if estimated > budget {
		return checkpointSummaryError("compaction_source_too_large", fmt.Sprintf("Checkpoint summary input is estimated at %d tokens; the input budget for model %q is %d tokens. No summary generation was started", estimated, model, budget))
	}
	return nil
}

// JSON escaping is transport syntax, not model context. Canonicalize only the
// measurement copy, retaining numbers exactly and preserving all input values.
// The outbound request, history identities and prompt-cache prefix are untouched.
// Existing tokenizer, formatting margin and image allowance still apply.
func checkpointContextSize(body []byte) (int, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil {
		return 0, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return 0, errors.New("unexpected trailing checkpoint request data")
	}
	var canonical bytes.Buffer
	encoder := json.NewEncoder(&canonical)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return 0, err
	}
	return unifiedContextSize(bytes.TrimSpace(canonical.Bytes())), nil
}
