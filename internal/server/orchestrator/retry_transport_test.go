package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

func TestTLSBadRecordMACRetryClassification(t *testing.T) {
	failure := &url.Error{Op: "Post", URL: "https://example.test/v1/responses", Err: &net.OpError{
		Op: "local error", Err: errors.New("tls: bad record MAC"),
	}}
	for _, scenario := range []struct {
		name string
		err  error
		want bool
	}{
		{name: "wrapped TLS record failure", err: fmt.Errorf("HTTP stream request failed: %w", failure), want: true},
		{name: "peer TLS record failure", err: errors.New("remote error: tls: bad record MAC"), want: true},
		{name: "unrelated text", err: errors.New("provider reported local error: tls: bad record MAC")},
		{name: "certificate verification", err: errors.New("tls: failed to verify certificate: x509: certificate signed by unknown authority")},
		{name: "HTTP 400", err: &httpclient.Error{StatusCode: 400, Body: []byte(`{"error":{"message":"local error: tls: bad record MAC"}}`)}},
		{name: "canceled", err: errors.Join(context.Canceled, failure)},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			require.Equal(t, scenario.want, isRetryableTransportError(scenario.err))
			require.Equal(t, scenario.want, IsUpstreamTransportError(scenario.err))
			if scenario.want {
				require.Equal(t, 502, ExtractStatusCodeFromError(ClassifyUpstreamTransportError(scenario.err)))
			}
		})
	}
}

func TestHTTP2PeerResetRetryClassification(t *testing.T) {
	for _, scenario := range []struct {
		name string
		err  error
		want bool
	}{
		{name: "internal reset", err: fmt.Errorf("failed to stream request: %w", errors.New("stream error: stream ID 65; INTERNAL_ERROR; received from peer")), want: true},
		{name: "refused stream", err: errors.New("stream error: stream ID 1; REFUSED_STREAM; received from peer"), want: true},
		{name: "local protocol violation", err: errors.New("stream error: stream ID 1; PROTOCOL_ERROR; received from peer")},
		{name: "local reset", err: errors.New("stream error: stream ID 1; INTERNAL_ERROR; local failure")},
		{name: "unrelated message", err: errors.New("provider says INTERNAL_ERROR")},
		{name: "structured bad request", err: &llm.ResponseError{StatusCode: 400, Detail: llm.ErrorDetail{Message: "stream error: stream ID 1; INTERNAL_ERROR; received from peer"}}},
		{name: "canceled", err: errors.Join(context.Canceled, errors.New("stream error: stream ID 1; INTERNAL_ERROR; received from peer"))},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			require.Equal(t, scenario.want, isRetryableTransportError(scenario.err))
			require.Equal(t, scenario.want, IsUpstreamTransportError(scenario.err))
		})
	}
}

func TestHTTP2PeerResetRetriesStickyLastChannel(t *testing.T) {
	candidate := &ChannelModelsCandidate{Channel: &biz.Channel{Channel: &ent.Channel{ID: 29}}, TraceSticky: true}
	state := &PersistenceState{CurrentCandidate: candidate, ChannelModelsCandidates: []*ChannelModelsCandidate{candidate}}
	outbound := &PersistentOutboundTransformer{state: state}
	failure := errors.New("stream error: stream ID 65; INTERNAL_ERROR; received from peer")
	require.True(t, outbound.CanRetry(failure))
	state.ChannelModelsCandidates = append(state.ChannelModelsCandidates, &ChannelModelsCandidate{Channel: &biz.Channel{Channel: &ent.Channel{ID: 30}}})
	require.False(t, outbound.CanRetry(failure))
}
