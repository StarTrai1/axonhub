package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// Give a slow but reading client enough time to receive large events. The
// allowance carries across writes because previously accepted bytes may still
// occupy the socket buffer. Callers serialize access to each budget.
const streamMinClientRate = 8 << 10

type streamWriteBudget struct {
	drainBy time.Time
}

func (b *streamWriteBudget) deadline(now time.Time, bytes int, idle time.Duration) time.Time {
	if b.drainBy.Before(now) {
		b.drainBy = now
	}
	b.drainBy = b.drainBy.Add(time.Duration(max(bytes, 0)) * time.Second / streamMinClientRate)
	return b.drainBy.Add(idle)
}

type sseBudgetWriter struct {
	gin.ResponseWriter
	budget streamWriteBudget
}

func (w *sseBudgetWriter) Write(data []byte) (int, error) {
	_ = http.NewResponseController(w.ResponseWriter).SetWriteDeadline(w.budget.deadline(time.Now(), len(data), sseWriteTimeout))
	return w.ResponseWriter.Write(data)
}

func (w *sseBudgetWriter) WriteString(data string) (int, error) {
	_ = http.NewResponseController(w.ResponseWriter).SetWriteDeadline(w.budget.deadline(time.Now(), len(data), sseWriteTimeout))
	return w.ResponseWriter.WriteString(data)
}

func (w *sseBudgetWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *sseBudgetWriter) FlushError() error {
	return flushResponseWriter(w.ResponseWriter)
}
