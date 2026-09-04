package httpclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/looplj/axonhub/llm/streams"
)

// MaxErrorBodySize is the maximum number of bytes read from an upstream error
// response body. Error bodies beyond this size are truncated to prevent OOM
// from pathological upstream responses that echo large request payloads in
// validation error messages, producing response bodies of 1+ GB.
const (
	MaxErrorBodySize       = 1 << 20 // 1 MB
	maxIdleConnsPerHost    = 100
	upstreamConnectTimeout = 10 * time.Second
)

// MaxHTTP2ConnectionShards bounds per-channel connection-pool fan-out.
const MaxHTTP2ConnectionShards = 8

// HTTPProtocol selects upstream HTTP protocol negotiation behavior.
type HTTPProtocol string

const (
	HTTPProtocolAuto  HTTPProtocol = "auto"
	HTTPProtocolHTTP1 HTTPProtocol = "http1"
)

func newUpstreamDialer() *net.Dialer {
	return &net.Dialer{
		Timeout:   upstreamConnectTimeout,
		KeepAlive: 30 * time.Second,
	}
}

// HttpClient implements the HttpClient interface.
type HttpClient struct {
	client               *http.Client
	proxyConfig          *ProxyConfig
	opts                 []ClientOption
	rejectHTTPSDowngrade bool
}

// ClientOption configures an HttpClient.
type ClientOption func(*clientOptions)

type clientOptions struct {
	insecureSkipVerify     bool
	httpProtocol          HTTPProtocol
	http2ConnectionShards int
	rejectHTTPSDowngrade   bool
}

// WithInsecureSkipVerify disables TLS certificate verification.
func WithInsecureSkipVerify(skip bool) ClientOption {
	return func(o *clientOptions) {
		o.insecureSkipVerify = skip
	}
}

// WithHTTPTransport configures upstream HTTP protocol negotiation and optional
// HTTP/2 connection-pool sharding. A single shard preserves the default transport.
func WithHTTPTransport(protocol HTTPProtocol, shards int) ClientOption {
	return func(o *clientOptions) {
		o.httpProtocol = protocol
		o.http2ConnectionShards = shards
	}
}

type shardedRoundTripper struct {
	transports []*http.Transport
	next       atomic.Uint64
}

func (s *shardedRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	index := (s.next.Add(1) - 1) % uint64(len(s.transports))
	return s.transports[index].RoundTrip(req)
}

func (s *shardedRoundTripper) CloseIdleConnections() {
	for _, transport := range s.transports {
		transport.CloseIdleConnections()
	}
}

func normalizeHTTPTransport(options clientOptions, disableConnectionReuse bool) (HTTPProtocol, int) {
	protocol := options.httpProtocol
	if protocol != HTTPProtocolHTTP1 {
		protocol = HTTPProtocolAuto
	}

	shards := options.http2ConnectionShards
	if shards < 1 {
		shards = 1
	} else if shards > MaxHTTP2ConnectionShards {
		shards = MaxHTTP2ConnectionShards
	}

	if protocol == HTTPProtocolHTTP1 || disableConnectionReuse {
		shards = 1
	}

	return protocol, shards
}

func configureHTTPTransport(
	transport *http.Transport,
	proxyConfig *ProxyConfig,
	options clientOptions,
	disableConnectionReuse bool,
) {
	transport.Proxy = getProxyFunc(proxyConfig)
	transport.DisableKeepAlives = disableConnectionReuse
	transport.DialContext = newUpstreamDialer().DialContext
	transport.MaxIdleConns = 100
	transport.MaxIdleConnsPerHost = maxIdleConnsPerHost
	transport.IdleConnTimeout = 90 * time.Second
	transport.TLSHandshakeTimeout = 10 * time.Second
	transport.ExpectContinueTimeout = 1 * time.Second

	protocol, _ := normalizeHTTPTransport(options, disableConnectionReuse)
	transport.ForceAttemptHTTP2 = protocol != HTTPProtocolHTTP1 && !disableConnectionReuse
	if protocol == HTTPProtocolHTTP1 {
		// An empty TLSNextProto map prevents automatic HTTP/2 negotiation.
		transport.TLSNextProto = make(map[string]func(string, *tls.Conn) http.RoundTripper)
	}

	if options.insecureSkipVerify {
		if transport.TLSClientConfig == nil {
			transport.TLSClientConfig = &tls.Config{}
		} else {
			transport.TLSClientConfig = transport.TLSClientConfig.Clone()
		}

		transport.TLSClientConfig.InsecureSkipVerify = true //nolint:gosec // User-configured option for self-signed certificates
	}

	if proxyURL := configuredHTTPSProxyURL(proxyConfig); proxyURL != nil {
		transport.DialTLSContext = buildHTTPSProxyDialTLSContext(
			proxyURL,
			transport.TLSClientConfig,
			transport.TLSHandshakeTimeout,
			transport.DialContext,
		)
	}
}

func configuredHTTPSProxyURL(config *ProxyConfig) *url.URL {
	if config == nil || config.Type != ProxyTypeURL {
		return nil
	}

	proxyURL, err := url.Parse(config.URL)
	if err != nil || !strings.EqualFold(proxyURL.Scheme, "https") {
		return nil
	}

	return proxyURL
}

func buildHTTPSProxyDialTLSContext(
	proxyURL *url.URL,
	baseTLS *tls.Config,
	handshakeTimeout time.Duration,
	baseDialContext func(context.Context, string, string) (net.Conn, error),
) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		rawConn, err := baseDialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}

		tlsConn := tls.Client(rawConn, httpsProxyTLSConfig(proxyURL, baseTLS))
		handshakeCtx := ctx
		if handshakeTimeout > 0 {
			var cancel context.CancelFunc
			handshakeCtx, cancel = context.WithTimeout(ctx, handshakeTimeout)
			defer cancel()
		}
		if err := tlsConn.HandshakeContext(handshakeCtx); err != nil {
			_ = rawConn.Close()
			return nil, fmt.Errorf("HTTPS proxy TLS handshake failed: %w", err)
		}

		return tlsConn, nil
	}
}

func httpsProxyTLSConfig(proxyURL *url.URL, baseTLS *tls.Config) *tls.Config {
	tlsConfig := &tls.Config{}
	if baseTLS != nil {
		tlsConfig = baseTLS.Clone()
	}
	if tlsConfig.ServerName == "" {
		tlsConfig.ServerName = proxyURL.Hostname()
	}
	// Go's proxy path sends an HTTP/1.1 CONNECT request. Do not negotiate h2
	// with an HTTPS proxy and then write an HTTP/1.1 request on that connection.
	tlsConfig.NextProtos = []string{"http/1.1"}

	return tlsConfig
}

func buildHTTPTransport(proxyConfig *ProxyConfig, options clientOptions, cloneDefault bool) *http.Transport {
	var transport *http.Transport
	if cloneDefault {
		if defaultTransport, ok := http.DefaultTransport.(*http.Transport); ok {
			transport = defaultTransport.Clone()
		}
	}
	if transport == nil {
		transport = &http.Transport{}
	}

	disableConnectionReuse := proxyConfig != nil &&
		proxyConfig.Type == ProxyTypeURL &&
		proxyConfig.DisableConnectionReuse
	configureHTTPTransport(transport, proxyConfig, options, disableConnectionReuse)

	return transport
}

func buildHTTPRoundTripper(proxyConfig *ProxyConfig, options clientOptions, cloneDefault bool) http.RoundTripper {
	disableConnectionReuse := proxyConfig != nil &&
		proxyConfig.Type == ProxyTypeURL &&
		proxyConfig.DisableConnectionReuse
	_, shards := normalizeHTTPTransport(options, disableConnectionReuse)

	if shards == 1 {
		return buildHTTPTransport(proxyConfig, options, cloneDefault)
	}

	transports := make([]*http.Transport, 0, shards)
	for range shards {
		transports = append(transports, buildHTTPTransport(proxyConfig, options, cloneDefault))
	}

	return &shardedRoundTripper{transports: transports}
}

// WithRejectHTTPSDowngrade rejects redirects from HTTPS to HTTP.
func WithRejectHTTPSDowngrade(reject bool) ClientOption {
	return func(o *clientOptions) {
		o.rejectHTTPSDowngrade = reject
	}
}

// NewHttpClientWithProxy creates a new HTTP client with proxy configuration.
func NewHttpClientWithProxy(proxyConfig *ProxyConfig, opts ...ClientOption) *HttpClient {
	var options clientOptions
	for _, opt := range opts {
		opt(&options)
	}
	client := &http.Client{Transport: buildHTTPRoundTripper(proxyConfig, options, proxyConfig == nil)}
	if options.rejectHTTPSDowngrade {
		client.CheckRedirect = rejectHTTPSDowngrade
	}

	return &HttpClient{
		client:               client,
		proxyConfig:          proxyConfig,
		opts:                 opts,
		rejectHTTPSDowngrade: options.rejectHTTPSDowngrade,
	}
}

// WithProxy returns a new HttpClient that uses the given proxy configuration,
// while preserving all other options (e.g., InsecureSkipVerify) from the original client.
func (hc *HttpClient) WithProxy(proxyConfig *ProxyConfig) *HttpClient {
	opts := append([]ClientOption(nil), hc.opts...)
	if hc.rejectHTTPSDowngrade {
		opts = append(opts, WithRejectHTTPSDowngrade(true))
	}
	return NewHttpClientWithProxy(proxyConfig, opts...)
}

// WithRejectHTTPSDowngrade returns a client that preserves all current options
// and rejects redirects that would send a request from HTTPS to HTTP.
func (hc *HttpClient) WithRejectHTTPSDowngrade() *HttpClient {
	client := *hc.client
	previousCheckRedirect := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if err := rejectHTTPSDowngrade(req, via); err != nil {
			return err
		}
		if previousCheckRedirect != nil {
			return previousCheckRedirect(req, via)
		}
		return nil
	}
	return &HttpClient{
		client:               &client,
		proxyConfig:          hc.proxyConfig,
		opts:                 hc.opts,
		rejectHTTPSDowngrade: true,
	}
}

func rejectHTTPSDowngrade(req *http.Request, via []*http.Request) error {
	if len(via) > 0 && via[len(via)-1].URL.Scheme == "https" && req.URL.Scheme == "http" {
		return fmt.Errorf("refusing HTTPS to HTTP redirect")
	}
	return nil
}

// WithHTTPTransport returns a new client with the requested transport policy,
// preserving proxy and TLS options from the original client.
func (hc *HttpClient) WithHTTPTransport(protocol HTTPProtocol, shards int) *HttpClient {
	opts := append([]ClientOption{}, hc.opts...)
	opts = append(opts, WithHTTPTransport(protocol, shards))
	return NewHttpClientWithProxy(hc.proxyConfig, opts...)
}

// GetNativeClient returns the underlying *http.Client for advanced use cases.
func (hc *HttpClient) GetNativeClient() *http.Client {
	return hc.client
}

// CloseIdleConnections closes idle connections held by the underlying HTTP transport.
func (hc *HttpClient) CloseIdleConnections() {
	hc.client.CloseIdleConnections()
}

func (hc *HttpClient) ProxyFunc() func(*http.Request) (*url.URL, error) {
	if hc == nil {
		return http.ProxyFromEnvironment
	}

	return getProxyFunc(hc.proxyConfig)
}

// getProxyFunc returns a proxy function based on the proxy configuration.
func getProxyFunc(config *ProxyConfig) func(*http.Request) (*url.URL, error) {
	// Handle nil config (backward compatibility) - default to environment
	if config == nil {
		return http.ProxyFromEnvironment
	}

	switch config.Type {
	case ProxyTypeDisabled:
		// No proxy - direct connection
		return func(*http.Request) (*url.URL, error) {
			return nil, nil
		}

	case ProxyTypeEnvironment:
		// Use environment variables (HTTP_PROXY, HTTPS_PROXY, NO_PROXY)
		return http.ProxyFromEnvironment

	case ProxyTypeURL:
		// Use configured URL with optional authentication
		if config.URL == "" {
			return func(*http.Request) (*url.URL, error) {
				return nil, errors.New("proxy URL is required when type is 'url'")
			}
		}

		proxyURL, err := url.Parse(config.URL)
		if err != nil {
			return func(_ *http.Request) (*url.URL, error) {
				return nil, fmt.Errorf("invalid proxy URL: %w", err)
			}
		}

		if config.Username != "" && config.Password != "" {
			proxyURL.User = url.UserPassword(config.Username, config.Password)
		}

		slog.DebugContext(context.Background(), "use custom proxy", slog.String("proxy_url", urlForLog(proxyURL)))

		return http.ProxyURL(proxyURL)

	default:
		// Unknown type - fall back to environment
		return http.ProxyFromEnvironment
	}
}

// NewHttpClient creates a new HTTP client.
func NewHttpClient(opts ...ClientOption) *HttpClient {
	var options clientOptions
	for _, opt := range opts {
		opt(&options)
	}

	return &HttpClient{
		client: &http.Client{Transport: buildHTTPRoundTripper(nil, options, true)},
		opts:   opts,
	}
}

// NewHttpClientWithClient creates a new HTTP client with a custom http.Client.
func NewHttpClientWithClient(client *http.Client) *HttpClient {
	return &HttpClient{
		client: client,
	}
}

// Do executes the HTTP request.
func (hc *HttpClient) Do(ctx context.Context, request *Request) (*Response, error) {
	rawReq, err := hc.BuildHttpRequest(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("failed to build HTTP request: %w", err)
	}
	slog.DebugContext(ctx, "execute http request",
		slog.String("method", rawReq.Method),
		slog.String("url", urlForLog(rawReq.URL)),
		slog.Int("body_size", len(request.Body)))

	// Only set the default Accept when the transformer did not specify one
	// (e.g. TTS sets Accept: */* to receive binary audio).
	if rawReq.Header.Get("Accept") == "" {
		rawReq.Header.Set("Accept", "application/json")
	}

	rawResp, err := hc.client.Do(rawReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}

	defer func() {
		err := rawResp.Body.Close()
		if err != nil {
			slog.WarnContext(ctx, "failed to close HTTP response body", slog.Any("error", err))
		}
	}()

	var body []byte
	// Cap error response bodies at 1 MB to prevent OOM from pathological
	// upstream error bodies (e.g., vLLM echoing multi-MB input in validation
	// errors). Successful responses are read in full because they are
	// typically small JSON payloads.
	if rawResp.StatusCode >= 400 {
		body, err = io.ReadAll(io.LimitReader(rawResp.Body, MaxErrorBodySize))
		if err != nil {
			return nil, fmt.Errorf("failed to read error response body: %w", err)
		}

		if slog.Default().Enabled(ctx, slog.LevelDebug) {
			slog.DebugContext(ctx, "HTTP request failed",
				slog.String("method", rawReq.Method),
				slog.String("url", urlForLog(rawReq.URL)),
				slog.Int("status_code", rawResp.StatusCode),
				slog.Int("body_size", len(body)))
		}

		return nil, &Error{
			Method:     rawReq.Method,
			URL:        rawReq.URL.String(),
			StatusCode: rawResp.StatusCode,
			Status:     rawResp.Status,
			Body:       body,
			Headers:    rawResp.Header,
		}
	}

	body, err = io.ReadAll(rawResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if slog.Default().Enabled(ctx, slog.LevelDebug) {
		slog.DebugContext(ctx, "HTTP request success",
			slog.String("method", rawReq.Method),
			slog.String("url", urlForLog(rawReq.URL)),
			slog.Int("status_code", rawResp.StatusCode),
			slog.Int("body_size", len(body)))
	}

	RecordResponseHeaders(ctx, rawResp.Header)

	// Build generic response
	response := &Response{
		StatusCode:  rawResp.StatusCode,
		Headers:     rawResp.Header,
		Body:        body,
		RawResponse: rawResp,
		Stream:      nil,
		Request:     request,
		RawRequest:  rawReq,
	}

	return response, nil
}

// DoStream executes a streaming HTTP request using Server-Sent Events.
func (hc *HttpClient) DoStream(ctx context.Context, request *Request) (streams.Stream[*StreamEvent], error) {
	streamCtx := ctx
	var streamCancel context.CancelFunc
	if request.DetachedStreamTimeout > 0 {
		streamCtx, streamCancel = context.WithTimeout(context.WithoutCancel(ctx), request.DetachedStreamTimeout)
	}

	rawReq, err := hc.BuildHttpRequest(streamCtx, request)
	if err != nil {
		if streamCancel != nil {
			streamCancel()
		}

		return nil, fmt.Errorf("failed to build HTTP request: %w", err)
	}
	slog.DebugContext(ctx, "execute stream request",
		slog.String("method", rawReq.Method),
		slog.String("url", urlForLog(rawReq.URL)),
		slog.Int("body_size", len(request.Body)))

	// Add streaming headers. Force SSE Accept unless the outbound transformer
	// explicitly opted into a non-JSON Accept (e.g. "*/*" for binary TTS chunks),
	// so chat-style outbounds whose default Accept is application/json still
	// negotiate SSE for streaming requests.
	accept := rawReq.Header.Get("Accept")
	if accept == "" || strings.EqualFold(accept, "application/json") {
		rawReq.Header.Set("Accept", "text/event-stream")
	}
	rawReq.Header.Set("Cache-Control", "no-cache")
	rawReq.Header.Set("Connection", "keep-alive")

	// Execute request
	rawResp, err := hc.client.Do(rawReq)
	if err != nil {
		if streamCancel != nil {
			streamCancel()
		}

		return nil, fmt.Errorf("HTTP stream request failed: %w", err)
	}

	// Check for HTTP errors before creating stream
	if rawResp.StatusCode >= 400 {
		defer func() {
			if streamCancel != nil {
				streamCancel()
			}

			err := rawResp.Body.Close()
			if err != nil {
				slog.WarnContext(ctx, "failed to close HTTP response body", slog.Any("error", err))
			}
		}()

		// Read error body for streaming requests
		body, err := io.ReadAll(io.LimitReader(rawResp.Body, MaxErrorBodySize))
		if err != nil {
			return nil, err
		}

		if slog.Default().Enabled(ctx, slog.LevelDebug) {
			slog.DebugContext(ctx, "HTTP stream request failed",
				slog.String("method", rawReq.Method),
				slog.String("url", urlForLog(rawReq.URL)),
				slog.Int("status_code", rawResp.StatusCode),
				slog.Int("body_size", len(body)))
		}

		return nil, &Error{
			Method:     rawReq.Method,
			URL:        rawReq.URL.String(),
			StatusCode: rawResp.StatusCode,
			Status:     rawResp.Status,
			Body:       body,
			Headers:    rawResp.Header,
		}
	}

	// Determine content type and select appropriate decoder
	contentType := rawResp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "text/event-stream" // Default to SSE
	}

	// Try to get a registered decoder for the content type
	decoderFactory, exists := GetDecoder(contentType)
	if !exists {
		if mediaType, _, err := mime.ParseMediaType(contentType); err == nil {
			decoderFactory, exists = GetDecoder(mediaType)
		}
	}
	if !exists {
		// Fallback to default SSE decoder
		slog.DebugContext(ctx, "no decoder found for content type, using default SSE", slog.String("content_type", contentType))

		decoderFactory = NewDefaultSSEDecoder
	}

	RecordResponseHeaders(streamCtx, rawResp.Header)

	stream := decoderFactory(streamCtx, rawResp.Body)
	if streamCancel != nil {
		stream = &detachedContextStream{
			Stream: stream,
			cancel: streamCancel,
		}
	}

	return stream, nil
}

type detachedContextStream struct {
	streams.Stream[*StreamEvent]
	cancel    context.CancelFunc
	closeErr  error
	closeOnce sync.Once
}

func (s *detachedContextStream) Next() bool {
	if s.Stream.Next() {
		return true
	}

	_ = s.Close()

	return false
}

func (s *detachedContextStream) Close() error {
	s.closeOnce.Do(func() {
		s.closeErr = s.Stream.Close()
		s.cancel()
	})

	return s.closeErr
}

func urlForLog(value *url.URL) string {
	if value == nil {
		return ""
	}
	redacted := *value
	redacted.User = nil
	redacted.RawQuery = ""
	redacted.ForceQuery = false
	redacted.Fragment = ""
	return redacted.String()
}

// BuildHttpRequest builds an HTTP request from Request.
func BuildHttpRequest(
	ctx context.Context,
	request *Request,
) (*http.Request, error) {
	var body io.Reader
	if len(request.Body) > 0 {
		body = bytes.NewReader(request.Body)
	}

	httpReq, err := http.NewRequestWithContext(ctx, request.Method, request.URL, body)
	if err != nil {
		return nil, err
	}

	httpReq.Header = request.Headers
	if httpReq.Header == nil {
		httpReq.Header = make(http.Header)
	}
	// Handle User-Agent header - only set default if not already present
	if httpReq.Header.Get("User-Agent") == "" {
		// No User-Agent set, use default
		httpReq.Header.Set("User-Agent", "axonhub/1.0")
	}

	for k := range libManagedHeaders {
		httpReq.Header.Del(k)
	}

	// Set Content-Type header if specified in request
	if request.ContentType != "" {
		httpReq.Header.Set("Content-Type", request.ContentType)
	}

	if request.Auth != nil {
		err = applyAuth(httpReq.Header, request.Auth)
		if err != nil {
			return nil, fmt.Errorf("failed to apply authentication: %w", err)
		}
	}

	if len(request.Query) > 0 {
		if httpReq.URL.RawQuery != "" {
			httpReq.URL.RawQuery += "&"
		}

		httpReq.URL.RawQuery += request.Query.Encode()
	}

	return httpReq, nil
}

// BuildHttpRequest builds an HTTP request from Request.
func (hc *HttpClient) BuildHttpRequest(
	ctx context.Context,
	request *Request,
) (*http.Request, error) {
	return BuildHttpRequest(ctx, request)
}

// applyAuth applies authentication to the HTTP request.
func applyAuth(headers http.Header, auth *AuthConfig) error {
	switch auth.Type {
	case "bearer":
		if auth.APIKey == "" {
			return fmt.Errorf("bearer token is required")
		}

		headers.Set("Authorization", "Bearer "+auth.APIKey)
	case "api_key":
		if auth.HeaderKey == "" {
			return fmt.Errorf("header key is required")
		}

		headers.Set(auth.HeaderKey, auth.APIKey)
	default:
		return fmt.Errorf("unsupported auth type: %s", auth.Type)
	}

	return nil
}

// extractHeaders extracts headers from HTTP response.
func (hc *HttpClient) extractHeaders(headers http.Header) map[string]string {
	result := make(map[string]string)

	for key, values := range headers {
		if len(values) > 0 {
			result[key] = values[0] // Take the first value
		}
	}

	return result
}
