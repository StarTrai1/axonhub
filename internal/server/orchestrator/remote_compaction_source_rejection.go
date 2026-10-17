package orchestrator

import (
	"github.com/looplj/axonhub/llm/httpclient"
)

type compactionSourceRejectedError struct {
	cause    error
	cacheKey string
}

func (e *compactionSourceRejectedError) Error() string { return e.cause.Error() }
func (e *compactionSourceRejectedError) Unwrap() error { return e.cause }

// Only opening-stream errors qualify. A partially generated summary must never
// be restarted through an ancestor fallback, nor may a generic 400 authorize it.
func rejectedCompactionSourceError(request *httpclient.Request, err error) error {
	if request == nil {
		return err
	}
	code, message, param, ok := responsesBadRequestDetails(err)
	if !ok {
		return err
	}
	ref, _, _, parseErr := parseRemoteCompactionRequest(request.Body)
	if parseErr != nil || ref == nil || isLocalCompactionReference(ref) {
		return err
	}
	exact := responsesRejectedCompactionMessage(request.Body, ref, code, message, param)
	hasIDs, safe := responsesResourceHistorySupportsRecoveryWithAgents(request.Body, exact)
	if !safe || !exact && (param != "" || hasIDs || !responsesResourceMismatch(code, message)) {
		return err
	}
	return &compactionSourceRejectedError{cause: err, cacheKey: remoteCompactionCacheKey(ref)}
}
