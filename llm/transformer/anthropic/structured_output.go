package anthropic

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/transformer"
)

// Restore the response schema after thinking normalization, which may replace
// output_config or remove unsupported effort levels. Schema constraints are
// independent of reasoning effort and must not disappear during that conversion.
func restoreStructuredOutput(src *llm.Request, dst *MessageRequest, config *Config) error {
	if src.ResponseFormat == nil || src.ResponseFormat.Type != "json_schema" {
		return nil
	}
	if config != nil {
		// Effort support alone does not imply format support. In particular,
		// DeepSeek documents output_config.effort only.
		//nolint:exhaustive // Only documented Claude destinations are enabled.
		switch config.Type {
		case "", PlatformDirect, PlatformClaudeCode, PlatformBedrock, PlatformVertex:
		default:
			return nil
		}
	}

	var wrapper struct {
		Schema json.RawMessage `json:"schema"`
	}
	if err := json.Unmarshal(src.ResponseFormat.JSONSchema, &wrapper); err != nil {
		return fmt.Errorf("%w: invalid structured output envelope: %w", transformer.ErrInvalidRequest, err)
	}
	// Unmarshal already validates the nested JSON and owns its RawMessage copy;
	// check the root shape without decoding or copying a potentially large schema again.
	schema := bytes.TrimSpace(wrapper.Schema)
	if len(schema) == 0 || schema[0] != '{' {
		return fmt.Errorf("%w: structured output schema must be a JSON object", transformer.ErrInvalidRequest)
	}
	if dst.OutputConfig == nil {
		dst.OutputConfig = &OutputConfig{}
	}
	// Keep the schema itself intact, including property order and constraints.
	// OpenAI's envelope name/description/strict are not Claude format fields.
	dst.OutputConfig.Format = &JSONOutputFormat{
		Type:   "json_schema",
		Schema: wrapper.Schema,
	}
	return nil
}
