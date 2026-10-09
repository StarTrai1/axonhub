package biz

import (
	"net/http"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/llm/transformer/shared"
)

// ResponsesSessionClientBody keeps the actual submitted history while restoring
// the client's context-window identity for replay. Provider identity policies or
// relay affinity recovery must not turn a same-window downstream continuation
// into an apparent new window, including after loading an execution from disk.
func ResponsesSessionClientBody(providerBody, clientBody []byte, clientHeaders http.Header) ([]byte, error) {
	metadata := gjson.GetBytes(clientBody, "client_metadata")
	body := providerBody
	var err error
	if metadata.Exists() {
		body, err = sjson.SetRawBytes(body, "client_metadata", []byte(metadata.Raw))
	} else {
		body, err = sjson.DeleteBytes(body, "client_metadata")
	}
	if err != nil {
		return nil, err
	}
	window := shared.ReadCodexRequestMetadata(clientHeaders, clientBody).WindowID
	if window != "" {
		return sjson.SetBytes(body, "client_metadata.x-codex-window-id", window)
	}
	return body, nil
}
