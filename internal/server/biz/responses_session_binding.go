package biz

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/schema/schematype"
	"github.com/looplj/axonhub/internal/ent/system"
)

type responsesSessionBinding struct {
	Version int    `json:"version"`
	Session string `json:"session"`
}

func responsesSessionBindingKey(scope [sha256.Size]byte) string {
	return "responses_session_binding_v1:" + hex.EncodeToString(scope[:])
}

func decodeResponsesSessionBinding(value string) (string, error) {
	var binding responsesSessionBinding
	if len(value) > 256 || json.Unmarshal([]byte(value), &binding) != nil || binding.Version != 1 {
		return "", fmt.Errorf("invalid persisted Responses session binding")
	}
	id, err := uuid.Parse(binding.Session)
	if err != nil || id == uuid.Nil || id.String() != binding.Session {
		return "", fmt.Errorf("invalid persisted Responses session alias")
	}
	return binding.Session, nil
}

// LoadResponsesSessionBinding reads an opaque alias keyed by a hash of the authenticated route identity.
// Bindings do not expire: reverting to an invalidated session after a restart or
// cache eviction would undo recovery. Each affected scope owns just one row.
func (service *SystemService) LoadResponsesSessionBinding(ctx context.Context, scope [sha256.Size]byte) (string, error) {
	ctx = authz.WithSystemBypass(ctx, "load-responses-session-binding")
	record, err := service.entFromContext(ctx).System.Query().Where(system.KeyEQ(responsesSessionBindingKey(scope))).Only(ctx)
	if ent.IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return decodeResponsesSessionBinding(record.Value)
}

// RenewResponsesSessionBinding replaces the alias observed by the rejected
// attempt. Concurrent or late failures reuse the winner instead of invalidating
// a newer binding. An empty expected value denotes the original client identity.
func (service *SystemService) RenewResponsesSessionBinding(ctx context.Context, scope [sha256.Size]byte, expected string) (string, error) {
	ctx = authz.WithSystemBypass(ctx, "renew-responses-session-binding")
	client := service.entFromContext(ctx)
	key := responsesSessionBindingKey(scope)
	for range 8 {
		record, err := client.System.Query().Where(system.KeyEQ(key)).Only(schematype.SkipSoftDelete(ctx))
		if err != nil && !ent.IsNotFound(err) {
			return "", err
		}
		current := ""
		if record != nil {
			if record.DeletedAt != 0 {
				return "", fmt.Errorf("Responses session binding was removed")
			}
			current, err = decodeResponsesSessionBinding(record.Value)
			if err != nil {
				return "", err
			}
		}
		if current != expected {
			if current == "" {
				return "", fmt.Errorf("Responses session binding changed during recovery")
			}
			return current, nil
		}
		next := uuid.NewString()
		encoded, err := json.Marshal(responsesSessionBinding{Version: 1, Session: next})
		if err != nil {
			return "", err
		}
		if record == nil {
			err = client.System.Create().SetKey(key).SetValue(string(encoded)).Exec(ctx)
			if ent.IsConstraintError(err) {
				continue
			}
			if err != nil {
				return "", err
			}
			return next, nil
		}
		changed, err := client.System.Update().Where(system.IDEQ(record.ID), system.ValueEQ(record.Value), system.UpdatedAtEQ(record.UpdatedAt)).SetValue(string(encoded)).Save(ctx)
		if err != nil {
			return "", err
		}
		if changed != 0 {
			return next, nil
		}
	}
	return "", fmt.Errorf("Responses session binding recovery exceeded concurrent update limit")
}
