package gc

import (
	"errors"
	"fmt"
	"time"
)

const (
	ResourceRequestPayloads  = "request_payloads"
	ResourceResponsePayloads = "response_payloads"
	ResourceRequests         = "requests"
	ResourceUsageLogs        = "usage_logs"
	ResourceChannelProbes     = "channel_probes"

	CleanupConfirmation = "DELETE"
)

var ErrCleanupAlreadyRunning = errors.New("a storage cleanup job is already running")

var supportedResourceTypes = map[string]struct{}{
	ResourceRequestPayloads:  {},
	ResourceResponsePayloads: {},
	ResourceRequests:         {},
	ResourceUsageLogs:        {},
	ResourceChannelProbes:     {},
}

var sensitiveResourceTypes = map[string]struct{}{
	ResourceRequestPayloads:  {},
	ResourceResponsePayloads: {},
	ResourceRequests:         {},
	ResourceUsageLogs:        {},
}

type CleanupSelection struct {
	ResourceType  string `json:"resourceType"`
	RetentionDays int    `json:"retentionDays"`
}

type ManualCleanupInput struct {
	Resources    []CleanupSelection `json:"resources"`
	Confirmation string             `json:"confirmation"`
}

type CleanupPreviewItem struct {
	ResourceType   string    `json:"resourceType"`
	EstimatedCount int       `json:"estimatedCount"`
	EstimatedBytes int64     `json:"estimatedBytes"`
	CutoffTime     time.Time `json:"cutoffTime"`
	RetentionDays  int       `json:"retentionDays"`
	Sensitive      bool      `json:"sensitive"`
}

type CleanupJobStatus struct {
	ID         string     `json:"id"`
	Status     string     `json:"status"`
	Phase      string     `json:"phase"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	Error      string     `json:"error,omitempty"`
}

func validateManualCleanupInput(input ManualCleanupInput, requireConfirmation bool) (map[string]int, bool, error) {
	if len(input.Resources) == 0 {
		return nil, false, errors.New("select at least one resource to clean")
	}

	manualDays := make(map[string]int, len(input.Resources))
	hasSensitive := false

	for _, selection := range input.Resources {
		resourceType := selection.ResourceType
		if _, ok := supportedResourceTypes[resourceType]; !ok {
			return nil, false, fmt.Errorf("unsupported cleanup resource %q", resourceType)
		}
		if selection.RetentionDays < 0 || selection.RetentionDays > 3650 {
			return nil, false, fmt.Errorf("retentionDays for %q must be between 0 and 3650", resourceType)
		}
		if _, exists := manualDays[resourceType]; exists {
			return nil, false, fmt.Errorf("cleanup resource %q is duplicated", resourceType)
		}

		manualDays[resourceType] = selection.RetentionDays
		if _, ok := sensitiveResourceTypes[resourceType]; ok {
			hasSensitive = true
		}
	}

	if requireConfirmation && hasSensitive && input.Confirmation != CleanupConfirmation {
		return nil, true, fmt.Errorf("type %q to confirm cleanup of sensitive data", CleanupConfirmation)
	}

	return manualDays, hasSensitive, nil
}

func cleanupCutoff(retentionDays int) time.Time {
	if retentionDays == 0 {
		return time.Now()
	}

	return time.Now().AddDate(0, 0, -retentionDays)
}
