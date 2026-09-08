package biz

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/samber/lo"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/schema/schematype"
	"github.com/looplj/axonhub/internal/ent/system"
)

const (
	ResponsesMetadataCapabilityTTL = 6 * time.Hour

	responsesMetadataCapabilitiesKey         = "responses_metadata_capabilities_v1"
	responsesMetadataCapabilitiesVersion     = 1
	responsesMetadataCapabilitiesLimit       = 1024
	responsesMetadataCapabilitiesMaxBytes    = 256 << 10
	responsesMetadataCapabilitiesMaxAttempts = 8
)

type responsesMetadataCapabilities struct {
	Version    int                  `json:"version"`
	Rejections map[string]time.Time `json:"rejections"`
}

func (service *SystemService) LoadResponsesMetadataRejection(ctx context.Context, scope [sha256.Size]byte) (time.Time, error) {
	ctx = authz.WithSystemBypass(ctx, "load-responses-metadata-capability")
	record, err := service.entFromContext(ctx).System.Query().
		Where(system.KeyEQ(responsesMetadataCapabilitiesKey)).
		Only(ctx)
	if ent.IsNotFound(err) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("load Responses metadata capabilities: %w", err)
	}

	capabilities, err := decodeResponsesMetadataCapabilities(record.Value, time.Now())
	if err != nil {
		return time.Time{}, err
	}

	return capabilities.Rejections[hex.EncodeToString(scope[:])], nil
}

func (service *SystemService) SaveResponsesMetadataRejection(ctx context.Context, scope [sha256.Size]byte, expiresAt time.Time) error {
	now := time.Now()
	if !expiresAt.After(now) || expiresAt.After(now.Add(ResponsesMetadataCapabilityTTL)) {
		return fmt.Errorf("Responses metadata capability expiry must be within six hours")
	}

	ctx = authz.WithSystemBypass(ctx, "save-responses-metadata-capability")
	client := service.entFromContext(ctx)
	scopeKey := hex.EncodeToString(scope[:])
	for range responsesMetadataCapabilitiesMaxAttempts {
		record, err := client.System.Query().Where(system.KeyEQ(responsesMetadataCapabilitiesKey)).Only(schematype.SkipSoftDelete(ctx))
		if err != nil && !ent.IsNotFound(err) {
			return fmt.Errorf("load Responses metadata capabilities for update: %w", err)
		}

		capabilities := responsesMetadataCapabilities{
			Version:    responsesMetadataCapabilitiesVersion,
			Rejections: make(map[string]time.Time),
		}
		if record != nil && record.DeletedAt == 0 {
			if retained, decodeErr := decodeResponsesMetadataCapabilities(record.Value, now); decodeErr == nil {
				capabilities = retained
			}
		}
		if expiresAt.After(capabilities.Rejections[scopeKey]) {
			capabilities.Rejections[scopeKey] = expiresAt
		}
		trimResponsesMetadataCapabilities(capabilities.Rejections)

		encoded, err := json.Marshal(capabilities)
		if err != nil {
			return fmt.Errorf("encode Responses metadata capabilities: %w", err)
		}
		if record == nil {
			err = client.System.Create().
				SetKey(responsesMetadataCapabilitiesKey).
				SetValue(string(encoded)).
				Exec(ctx)
			if ent.IsConstraintError(err) {
				continue
			}
			if err != nil {
				return fmt.Errorf("create Responses metadata capabilities: %w", err)
			}
			return nil
		}
		if record.DeletedAt == 0 && record.Value == string(encoded) {
			return nil
		}

		affected, err := client.System.Update().
			Where(
				system.IDEQ(record.ID),
				system.UpdatedAtEQ(record.UpdatedAt),
				system.ValueEQ(record.Value),
			).
			SetDeletedAt(0).
			SetValue(string(encoded)).
			Save(ctx)
		if err != nil {
			return fmt.Errorf("update Responses metadata capabilities: %w", err)
		}
		if affected != 0 {
			return nil
		}
	}

	return fmt.Errorf("update Responses metadata capabilities exceeded %d concurrent attempts", responsesMetadataCapabilitiesMaxAttempts)
}

func decodeResponsesMetadataCapabilities(value string, now time.Time) (responsesMetadataCapabilities, error) {
	var capabilities responsesMetadataCapabilities
	if len(value) > responsesMetadataCapabilitiesMaxBytes {
		return capabilities, fmt.Errorf("persisted Responses metadata capabilities exceed size limit")
	}
	if err := json.Unmarshal([]byte(value), &capabilities); err != nil {
		return capabilities, fmt.Errorf("decode persisted Responses metadata capabilities: %w", err)
	}
	if capabilities.Version != responsesMetadataCapabilitiesVersion || capabilities.Rejections == nil ||
		len(capabilities.Rejections) > responsesMetadataCapabilitiesLimit {
		return capabilities, fmt.Errorf("invalid persisted Responses metadata capability version or shape")
	}

	for scope, expiresAt := range capabilities.Rejections {
		decoded, err := hex.DecodeString(scope)
		if err != nil || len(decoded) != sha256.Size || scope != strings.ToLower(scope) ||
			!expiresAt.After(now) || expiresAt.After(now.Add(ResponsesMetadataCapabilityTTL)) {
			delete(capabilities.Rejections, scope)
		}
	}
	return capabilities, nil
}

func trimResponsesMetadataCapabilities(rejections map[string]time.Time) {
	if len(rejections) <= responsesMetadataCapabilitiesLimit {
		return
	}
	keys := lo.Keys(rejections)
	slices.SortFunc(keys, func(left, right string) int {
		if order := rejections[left].Compare(rejections[right]); order != 0 {
			return order
		}
		return strings.Compare(left, right)
	})
	for _, scope := range keys[:len(keys)-responsesMetadataCapabilitiesLimit] {
		delete(rejections, scope)
	}
}
