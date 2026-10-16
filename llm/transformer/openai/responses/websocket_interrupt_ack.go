package responses

import (
 "encoding/json"
 "time"
)

const interruptAcknowledgementGrace = time.Second

func (s *webSocketStream) observeInterruptAcknowledgement(raw []byte) {
 var ack struct { Type string `json:"type"`; ResponseID string `json:"response_id"` }
 if json.Unmarshal(raw, &ack) != nil { return }
 s.mu.Lock()
 defer s.mu.Unlock()
 if _, pending := s.interruptPending[ack.ResponseID]; !pending { return }
 delete(s.interruptPending, ack.ResponseID)
 if ack.Type == "response.interrupt.failed" { s.retireAfterTerminal = true }
}

// drainTerminalInterrupt keeps the terminal response authoritative while
// validating the late acknowledgement before reusing the upstream socket.
func (s *webSocketStream) drainTerminalInterrupt(lease *webSocketLease, terminal []byte) []byte {
 id := responseIDFromWebSocketEvent(terminal)
 s.mu.Lock()
 _, pending := s.interruptPending[id]
 delete(s.interruptPending, id)
 s.mu.Unlock()
 if !pending { return nil }
 deadline := time.Now().Add(interruptAcknowledgementGrace)
 if end, ok := s.ctx.Deadline(); ok && end.Before(deadline) { deadline = end }
 if lease.conn.SetReadDeadline(deadline) != nil {
  s.retireAfterTerminal = true
  return nil
 }
 _, raw, err := lease.conn.ReadMessage()
 _ = lease.conn.SetReadDeadline(time.Time{})
 var ack struct { Type string `json:"type"`; ResponseID string `json:"response_id"` }
 if err != nil || json.Unmarshal(raw, &ack) != nil || ack.ResponseID != id ||
  (ack.Type != "response.interrupt.accepted" && ack.Type != "response.interrupt.failed") {
  s.retireAfterTerminal = true
  return nil
 }
 s.retireAfterTerminal = ack.Type == "response.interrupt.failed"
 return raw
}
