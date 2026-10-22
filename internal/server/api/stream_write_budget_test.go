package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func TestStreamWriteBudgetCarriesQueuedBytesAndExpires(t *testing.T) {
	now := time.Unix(1000, 0)
	var budget streamWriteBudget
	first := budget.deadline(now, 1<<20, 30*time.Second)
	require.Equal(t, now.Add(158*time.Second), first)
	second := budget.deadline(now.Add(time.Second), 1<<20, 30*time.Second)
	require.Equal(t, now.Add(286*time.Second), second)
	// A heartbeat cannot discard the outstanding transfer allowance.
	require.Equal(t, second, budget.deadline(now.Add(2*time.Second), 0, 30*time.Second))
	// After the client has had time to drain, small writes regain the idle bound.
	require.Equal(t, now.Add(331*time.Second), budget.deadline(now.Add(300*time.Second), 8192, 30*time.Second))
}

func TestSSELargeEventsKeepBudgetAcrossFlushes(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	tracking := &deadlineTrackingResponseWriter{ResponseWriter: c.Writer}
	c.Writer = tracking
	setSSEHeaders(c)
	data := bytes.Repeat([]byte("x"), 1<<20)
	started := time.Now()
	require.NoError(t, writeSSEEvent(c.Request.Context(), c.Writer, "large", data))
	var first time.Time
	for _, d := range tracking.deadlines {
		if d.After(first) {
			first = d
		}
	}
	require.True(t, first.After(started.Add(150*time.Second)))
	require.NoError(t, writeSSEEvent(c.Request.Context(), c.Writer, "large", data))
	var last time.Time
	for _, d := range tracking.deadlines {
		if d.After(last) {
			last = d
		}
	}
	require.True(t, last.After(first.Add(127*time.Second)))
	require.Equal(t, 2, bytes.Count(recorder.Body.Bytes(), data))
	requireProtectedSSEWrites(t, tracking)
}

type budgetWebSocketWriter struct {
	deadline time.Time
	writes   int
}

func (w *budgetWebSocketWriter) SetWriteDeadline(deadline time.Time) error {
	w.deadline = deadline
	return nil
}
func (w *budgetWebSocketWriter) WriteMessage(_ int, _ []byte) error { w.writes++; return nil }

func TestWebSocketLargeEventsShareConnectionBudget(t *testing.T) {
	conn := &budgetWebSocketWriter{}
	writer := &responsesWebSocketWriter{conn: conn}
	started := time.Now()
	require.NoError(t, writeResponsesWebSocketMessage(writer, websocket.TextMessage, make([]byte, 1<<20)))
	first := conn.deadline
	require.True(t, first.After(started.Add(130*time.Second)))
	require.NoError(t, writeResponsesWebSocketMessage(writer, websocket.TextMessage, make([]byte, 1<<20)))
	require.True(t, conn.deadline.After(first.Add(127*time.Second)))
	require.Zero(t, writer.activeWriteDeadline.Load())
	require.Equal(t, 2, conn.writes)
}
