package orchestrator

import (
	"context"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/internal/authz"
	entchannel "github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

func responsesAgentTool(namespace, name string) bool {
	return (namespace == "agents" || namespace == "collaboration") &&
		(name == "send_message" || name == "spawn_agent" || name == "followup_task")
}

// Request plaintext only for the dedicated message parameter, then seal that
// exact parameter for the client. The gateway can reopen it on any channel in
// the same installation and authenticated owner scope, including after restart.
func portableResponsesAgentTransport(outbound *PersistentOutboundTransformer, service *biz.SystemService) pipeline.Middleware {
	return &responsesAgentTransport{outbound: outbound, service: service}
}

type responsesAgentTransport struct {
	pipeline.DummyMiddleware
	outbound *PersistentOutboundTransformer
	service  *biz.SystemService
	tools    map[string]bool
	aead     cipher.AEAD
	aad      []byte
	scope    string
	dirty    bool
}

func (m *responsesAgentTransport) Name() string { return "responses-portable-agent-transport" }

func (m *responsesAgentTransport) OnOutboundRawRequest(ctx context.Context, request *httpclient.Request) (*httpclient.Request, error) {
	m.tools = nil
	m.scope = ""
	m.dirty = false
	if m.outbound == nil || m.outbound.state == nil || m.service == nil || request == nil ||
		(request.APIFormat != string(llm.APIFormatOpenAIResponse) && request.APIFormat != string(llm.APIFormatOpenAIResponseCompact)) {
		return request, nil
	}
	state := m.outbound.state
	channel := m.outbound.GetCurrentChannel()
	if channel == nil || channel.Type != entchannel.TypeCodex || state.APIKey == nil || state.APIKey.ID <= 0 || state.APIKey.ProjectID <= 0 {
		return request, nil
	}
	metadata := shared.ReadCodexRequestMetadata(request.Headers, request.Body)
	if metadata.ThreadID != "" {
		provider := responsesMetadataKey(channel.ID, request)
		credential := hex.EncodeToString(provider.credential[:])
		if channel.Credentials.IsOAuth() && request.Headers.Get("Chatgpt-Account-Id") != "" {
			credential = "oauth-account:" + request.Headers.Get("Chatgpt-Account-Id")
		}
		endpoint := strings.TrimSuffix(provider.url, "/compact")
		if parsed, err := url.Parse(endpoint); err == nil {
			if parsed.Scheme == "wss" {
				parsed.Scheme = "https"
			} else if parsed.Scheme == "ws" {
				parsed.Scheme = "http"
			}
			endpoint = parsed.String()
		}
		identity, err := json.Marshal([]any{state.APIKey.ProjectID, state.APIKey.ID, provider.channelID, endpoint, provider.model, credential, metadata.ThreadID, metadata.SessionID})
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256(identity)
		m.scope = hex.EncodeToString(digest[:])
		lookupCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		policy, err := m.service.LoadAgentTransportPolicy(lookupCtx, m.scope)
		cancel()
		if err != nil {
			return nil, err
		}
		if policy != "" {
			var names []string
			if err := json.Unmarshal([]byte(policy), &names); err != nil || len(names) > 6 {
				return nil, errors.New("invalid saved agent transport policy")
			}
			m.tools = make(map[string]bool)
			for _, name := range names {
				namespace, tool, ok := strings.Cut(name, ".")
				if !ok || !responsesAgentTool(namespace, tool) {
					return nil, errors.New("invalid saved agent transport tool")
				}
				m.tools[name] = true
			}
		}
	}
	paths := []string{"tools"}
	for i, item := range gjson.GetBytes(request.Body, "input").Array() {
		if item.Get("type").String() == "additional_tools" {
			paths = append(paths, fmt.Sprintf("input.%d.tools", i))
		}
	}
	body := request.Body
	for _, path := range paths {
		for i, namespace := range gjson.GetBytes(body, path).Array() {
			if namespace.Get("type").String() != "namespace" {
				continue
			}
			for j, tool := range namespace.Get("tools").Array() {
				name, ns := tool.Get("name").String(), namespace.Get("name").String()
				if tool.Get("type").String() != "function" || !responsesAgentTool(ns, name) ||
					tool.Get("parameters.properties.message.type").String() != "string" ||
					(tool.Get("parameters.properties.message.encrypted").Type != gjson.True && tool.Get("parameters.properties.message.encrypted").Type != gjson.False) {
					continue
				}
				var err error
				body, err = sjson.SetBytes(body, fmt.Sprintf("%s.%d.tools.%d.parameters.properties.message.encrypted", path, i, j), false)
				if err != nil {
					return nil, err
				}
				if m.tools == nil {
					m.tools = make(map[string]bool)
				}
				m.dirty = m.dirty || !m.tools[ns+"."+name]
				m.tools[ns+"."+name] = true
			}
		}
	}
	if len(m.tools) == 0 {
		return request, nil
	}
	secret, err := m.service.SecretKey(authz.WithSystemBypass(ctx, "agent-message-encryption-key"))
	if err != nil {
		return nil, err
	}
	m.aead, err = newResponsesAgentMessageCipher(secret)
	if err != nil {
		return nil, err
	}
	m.aad, err = responsesAgentMessageAAD(state, "")
	if err != nil {
		return nil, err
	}
	updated := *request
	updated.Body = body
	if len(request.JSONBody) > 0 {
		updated.JSONBody = body
	}
	return &updated, nil
}

func (m *responsesAgentTransport) confirm(ctx context.Context) error {
	if !m.dirty || m.scope == "" {
		return nil
	}
	names := make([]string, 0, len(m.tools))
	for name := range m.tools {
		names = append(names, name)
	}
	policy, err := json.Marshal(names)
	if err != nil {
		return err
	}
	writeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := m.service.SaveAgentTransportPolicy(writeCtx, m.scope, string(policy)); err != nil {
		return err
	}
	m.dirty = false
	return nil
}

func (m *responsesAgentTransport) selected(item gjson.Result) bool {
	return item.Get("type").String() == "function_call" && m.tools[item.Get("namespace").String()+"."+item.Get("name").String()]
}

type responsesAgentSealedCall struct {
	plaintext string
	sealed    string
}

func (m *responsesAgentTransport) sealCall(item gjson.Result, calls map[string]responsesAgentSealedCall) ([]byte, error) {
	if len(item.Get("encrypted_function_args").Array()) != 0 {
		return nil, errors.New("upstream did not return the requested portable agent message")
	}
	id, arguments := item.Get("id").String(), item.Get("arguments").String()
	message := gjson.Get(arguments, "message")
	if id == "" || !gjson.Valid(arguments) || message.Type != gjson.String {
		return nil, errors.New("upstream returned an invalid portable agent call")
	}
	previous, found := calls[id]
	if found && previous.plaintext != message.String() {
		return nil, errors.New("upstream changed a completed agent message")
	}
	if !found {
		sealed, err := sealResponsesAgentMessage(m.aead, m.aad, message.String())
		if err != nil {
			return nil, err
		}
		previous = responsesAgentSealedCall{plaintext: message.String(), sealed: sealed}
		calls[id] = previous
	}
	arguments, err := sjson.Set(arguments, "message", previous.sealed)
	if err != nil {
		return nil, err
	}
	body, err := sjson.Set(item.Raw, "arguments", arguments)
	if err != nil {
		return nil, err
	}
	// Explicitly preserve encrypted delivery even for Codex clients that
	// recognize an empty encrypted_function_args list as plaintext.
	body, err = sjson.Set(body, "encrypted_function_args", []string{"message"})
	return []byte(body), err
}

func (m *responsesAgentTransport) sealOutput(body []byte, path string, calls map[string]responsesAgentSealedCall) ([]byte, error) {
	for i, item := range gjson.GetBytes(body, path).Array() {
		if !m.selected(item) {
			continue
		}
		sealed, err := m.sealCall(item, calls)
		if err != nil {
			return nil, err
		}
		body, err = sjson.SetRawBytes(body, fmt.Sprintf("%s.%d", path, i), sealed)
		if err != nil {
			return nil, err
		}
	}
	return body, nil
}

func (m *responsesAgentTransport) OnOutboundRawResponse(ctx context.Context, response *httpclient.Response) (*httpclient.Response, error) {
	if len(m.tools) == 0 || response == nil {
		return response, nil
	}
	body, err := m.sealOutput(response.Body, "output", make(map[string]responsesAgentSealedCall))
	if err != nil {
		return nil, err
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		if err := m.confirm(ctx); err != nil {
			return nil, err
		}
	}
	updated := *response
	updated.Body = body
	return &updated, nil
}

func (m *responsesAgentTransport) OnOutboundRawStream(ctx context.Context, stream streams.Stream[*httpclient.StreamEvent]) (streams.Stream[*httpclient.StreamEvent], error) {
	if len(m.tools) == 0 {
		return stream, nil
	}
	return &responsesAgentTransportStream{Stream: stream, ctx: ctx, transport: m, active: make(map[string]bool), calls: make(map[string]responsesAgentSealedCall)}, nil
}

type responsesAgentTransportStream struct {
	streams.Stream[*httpclient.StreamEvent]
	ctx       context.Context
	transport *responsesAgentTransport
	active    map[string]bool
	calls     map[string]responsesAgentSealedCall
	pending   []*httpclient.StreamEvent
	current   *httpclient.StreamEvent
	err       error
	sequence  int
}

func (s *responsesAgentTransportStream) Next() bool {
	for len(s.pending) == 0 && s.err == nil {
		if !s.Stream.Next() {
			return false
		}
		s.pending, s.err = s.transform(s.Stream.Current())
	}
	if s.err != nil {
		return false
	}
	s.current = s.pending[0]
	s.pending = s.pending[1:]
	if s.current != nil && gjson.GetBytes(s.current.Data, "sequence_number").Exists() {
		copyEvent := *s.current
		copyEvent.Data, s.err = sjson.SetBytes(copyEvent.Data, "sequence_number", s.sequence)
		if s.err != nil {
			return false
		}
		s.sequence++
		s.current = &copyEvent
	}
	return true
}

func (s *responsesAgentTransportStream) Current() *httpclient.StreamEvent { return s.current }

func (s *responsesAgentTransportStream) Err() error {
	if s.err != nil {
		return s.err
	}
	return s.Stream.Err()
}

func (s *responsesAgentTransportStream) transform(event *httpclient.StreamEvent) ([]*httpclient.StreamEvent, error) {
	if event == nil {
		return nil, nil
	}
	data := gjson.ParseBytes(event.Data)
	item := data.Get("item")
	kind := data.Get("type").String()
	updated := *event
	switch kind {
	case "response.created":
		if err := s.transport.confirm(s.ctx); err != nil {
			return nil, err
		}
	case "response.output_item.added":
		if s.transport.selected(item) {
			if len(s.active) >= 128 || item.Get("id").String() == "" {
				return nil, errors.New("invalid portable agent stream identity")
			}
			s.active[item.Get("id").String()] = true
			var err error
			updated.Data, err = sjson.SetBytes(updated.Data, "item.arguments", "")
			if err != nil {
				return nil, err
			}
		}
	case "response.function_call_arguments.delta", "response.function_call_arguments.done":
		if s.active[data.Get("item_id").String()] {
			// No plaintext argument fragment is delivered to the client. A
			// complete sealed delta and done event are emitted at item completion.
			return nil, nil
		}
	case "response.output_item.done":
		if s.transport.selected(item) {
			sealed, err := s.transport.sealCall(item, s.calls)
			if err != nil {
				return nil, err
			}
			updated.Data, err = sjson.SetRawBytes(event.Data, "item", sealed)
			if err != nil {
				return nil, err
			}
			arguments := gjson.GetBytes(sealed, "arguments").String()
			var events []*httpclient.StreamEvent
			for _, eventType := range []string{"response.function_call_arguments.delta", "response.function_call_arguments.done"} {
				value := map[string]any{"type": eventType, "item_id": item.Get("id").String(), "output_index": data.Get("output_index").Int()}
				if data.Get("sequence_number").Exists() {
					value["sequence_number"] = 0
				}
				if eventType == "response.function_call_arguments.delta" {
					value["delta"] = arguments
				} else {
					value["arguments"] = arguments
				}
				body, err := json.Marshal(value)
				if err != nil {
					return nil, err
				}
				events = append(events, &httpclient.StreamEvent{Type: eventType, Data: body})
			}
			return append(events, &updated), nil
		}
	case "response.completed", "response.incomplete", "response.failed":
		var err error
		updated.Data, err = s.transport.sealOutput(event.Data, "response.output", s.calls)
		if err != nil {
			return nil, err
		}
	}
	return []*httpclient.StreamEvent{&updated}, nil
}
