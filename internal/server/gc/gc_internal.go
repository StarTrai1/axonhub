package gc

import (
	"context"
	"errors"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/log"
)

func (w *Worker) runAutomaticCleanup(ctx context.Context) {
	ctx = authz.WithSystemBypass(ctx, "gc-cleanup")
	if err := w.runCleanup(ctx, false, nil); err != nil {
		if errors.Is(err, ErrCleanupAlreadyRunning) {
			log.Info(ctx, "Skipping scheduled cleanup because another cleanup is running")
			return
		}
		log.Error(ctx, "Scheduled cleanup failed", log.Cause(err))
	}
}
