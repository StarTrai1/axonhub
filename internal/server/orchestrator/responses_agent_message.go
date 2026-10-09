package orchestrator

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/internal/authz"
	entchannel "github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
)

const (
	responsesAgentMessagePrefix   = "axonhub-agent-v1."
	responsesAgentMessageMaxBytes = 1024 * 1024
)

func newResponsesAgentMessageCipher(secret string) (cipher.AEAD, error) {
	if secret == "" {
		return nil, errors.New("agent message encryption key is unavailable")
	}
	key := hmac.New(sha256.New, []byte(secret))
	_, _ = key.Write([]byte("axonhub/agent-message/v1"))
	block, err := aes.NewCipher(key.Sum(nil))
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func responsesAgentMessageAAD(state *PersistenceState, recovery string) ([]byte, error) {
	if state == nil || state.APIKey == nil || state.APIKey.ID <= 0 || state.APIKey.ProjectID <= 0 {
		return nil, errors.New("agent message recovery requires an authenticated API key and project")
	}
	return fmt.Appendf(nil, "axonhub/agent-message/v1:%d:%d:%s", state.APIKey.ProjectID, state.APIKey.ID, recovery), nil
}

func sealResponsesAgentMessage(aead cipher.AEAD, aad []byte, plaintext string) (string, error) {
	if aead == nil || len(aad) == 0 || len(plaintext) > responsesAgentMessageMaxBytes ||
		strings.TrimSpace(plaintext) == "" || !utf8.ValidString(plaintext) {
		return "", errors.New("invalid agent message to seal")
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := aead.Seal(nonce, nonce, []byte(plaintext), aad)
	return responsesAgentMessagePrefix + base64.RawURLEncoding.EncodeToString(sealed), nil
}

func openResponsesAgentMessage(aead cipher.AEAD, aad []byte, sealed string) (string, error) {
	invalid := errors.New("agent message recovery could not be authenticated for this API key and project")
	if aead == nil || len(aad) == 0 || !strings.HasPrefix(sealed, responsesAgentMessagePrefix) {
		return "", invalid
	}
	encoded := strings.TrimPrefix(sealed, responsesAgentMessagePrefix)
	if len(encoded) > base64.RawURLEncoding.EncodedLen(responsesAgentMessageMaxBytes+aead.NonceSize()+aead.Overhead()) {
		return "", invalid
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(payload) < aead.NonceSize()+aead.Overhead() {
		return "", invalid
	}
	plaintext, err := aead.Open(nil, payload[:aead.NonceSize()], payload[aead.NonceSize():], aad)
	if err != nil || !utf8.Valid(plaintext) || len(bytes.TrimSpace(plaintext)) == 0 {
		return "", invalid
	}
	return string(plaintext), nil
}

// The imported plaintext is bound to the entire message identity, not just a
// reusable upstream ID. Later final reports cannot satisfy this lookup.
func responsesAgentMessageRecoveryDigest(item gjson.Result, ciphertext string) string {
	if item.Get("id").String() == "" || item.Get("author").String() == "" || item.Get("recipient").String() == "" || ciphertext == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(ciphertext))
	identity, _ := json.Marshal([]string{item.Get("id").String(), item.Get("author").String(), item.Get("recipient").String(), hex.EncodeToString(sum[:])})
	digest := sha256.Sum256(identity)
	return hex.EncodeToString(digest[:])
}

func recoverResponsesAgentMessages(outbound *PersistentOutboundTransformer, service *biz.SystemService) pipeline.Middleware {
	return pipeline.OnRawRequest("responses-agent-message-recovery", func(ctx context.Context, request *httpclient.Request) (*httpclient.Request, error) {
		if outbound == nil || outbound.state == nil || service == nil || request == nil ||
			(request.APIFormat != string(llm.APIFormatOpenAIResponse) && request.APIFormat != string(llm.APIFormatOpenAIResponseCompact)) {
			return request, nil
		}
		channel := outbound.GetCurrentChannel()
		state := outbound.state
		if channel == nil || channel.Type != entchannel.TypeCodex || state.APIKey == nil || state.APIKey.ID <= 0 || state.APIKey.ProjectID <= 0 {
			return request, nil
		}
		body := request.Body
		var aead cipher.AEAD
		lookupCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		loadCipher := func() error {
			if aead != nil {
				return nil
			}
			secret, err := service.SecretKey(authz.WithSystemBypass(lookupCtx, "agent-message-encryption-key"))
			if err != nil {
				return err
			}
			aead, err = newResponsesAgentMessageCipher(secret)
			return err
		}
		for i, item := range gjson.GetBytes(body, "input").Array() {
			if item.Get("type").String() == "function_call" && responsesAgentTool(item.Get("namespace").String(), item.Get("name").String()) {
				arguments := item.Get("arguments").String()
				sealed := gjson.Get(arguments, "message").String()
				if strings.HasPrefix(sealed, responsesAgentMessagePrefix) {
					if err := loadCipher(); err != nil {
						return nil, err
					}
					aad, err := responsesAgentMessageAAD(state, "")
					if err != nil {
						return nil, err
					}
					plaintext, err := openResponsesAgentMessage(aead, aad, sealed)
					if err != nil {
						return nil, err
					}
					arguments, err = sjson.Set(arguments, "message", plaintext)
					if err != nil {
						return nil, err
					}
					body, err = sjson.SetBytes(body, fmt.Sprintf("input.%d.arguments", i), arguments)
					if err != nil {
						return nil, err
					}
					body, err = sjson.DeleteBytes(body, fmt.Sprintf("input.%d.encrypted_function_args", i))
					if err != nil {
						return nil, err
					}
				}
			}
			if item.Get("type").String() != "agent_message" {
				continue
			}
			for j, part := range item.Get("content").Array() {
				if part.Get("type").String() != "encrypted_content" {
					continue
				}
				sealed := part.Get("encrypted_content").String()
				recovery := ""
				if !strings.HasPrefix(sealed, responsesAgentMessagePrefix) {
					recovery = responsesAgentMessageRecoveryDigest(item, sealed)
					if recovery == "" {
						continue
					}
					var err error
					sealed, err = service.LoadAgentMessageRecovery(lookupCtx, state.APIKey.ProjectID, state.APIKey.ID, recovery)
					if err != nil {
						return nil, fmt.Errorf("load agent message recovery: %w", err)
					}
					if sealed == "" {
						continue
					}
				}
				if err := loadCipher(); err != nil {
					return nil, err
				}
				aad, err := responsesAgentMessageAAD(state, recovery)
				if err != nil {
					return nil, err
				}
				plaintext, err := openResponsesAgentMessage(aead, aad, sealed)
				if err != nil {
					return nil, err
				}
				path := fmt.Sprintf("input.%d.content.%d", i, j)
				restored, err := sjson.Set(part.Raw, "type", "input_text")
				if err != nil {
					return nil, err
				}
				restored, err = sjson.Set(restored, "text", plaintext)
				if err != nil {
					return nil, err
				}
				restored, err = sjson.Delete(restored, "encrypted_content")
				if err != nil {
					return nil, err
				}
				body, err = sjson.SetRawBytes(body, path, []byte(restored))
				if err != nil {
					return nil, err
				}
			}
		}
		if bytes.Equal(body, request.Body) {
			return request, nil
		}
		updated := *request
		updated.Body = body
		if len(request.JSONBody) > 0 {
			updated.JSONBody = body
		}
		updated.Headers = request.Headers.Clone()
		updated.Headers.Del(codexTurnStateHeader)
		return &updated, nil
	})
}
