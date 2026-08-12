package publicapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	maxDecodedBodyBytes          = 4 << 20
	maxDiagnosticBodyBytes       = 64 << 10
	maxSSEEventBytes             = 4 << 20
	defaultResponseTimeout       = 30 * time.Second
	defaultSSEIdleTimeout        = 60 * time.Second
	defaultSSEMaxRetries         = 5
	defaultSSEReconnectBaseDelay = 200 * time.Millisecond
	maxSSERetries                = 100
)

type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type HTTPClientFunc func(*http.Request) (*http.Response, error)

func (f HTTPClientFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

// RequestEditorFn mutates a request immediately before transport execution.
// The context is the operation context, including its response timeout.
type RequestEditorFn func(context.Context, *http.Request) error

type Option func(*ClientOptions)

type ClientOptions struct {
	BaseURL                 string
	UploadBaseURL           string
	httpClient              HTTPClient
	requestEditors          []RequestEditorFn
	responseTimeout         time.Duration
	sseIdleTimeout          time.Duration
	sseMaxRetries           int
	sseReconnectBaseDelay   time.Duration
	sseReconnectOnStreamEnd bool
}

// WithUploadBaseURL configures the origin used by generated resumable upload
// initiation methods. An empty value uses BaseURL.
func WithUploadBaseURL(baseURL string) Option {
	return func(options *ClientOptions) {
		options.UploadBaseURL = baseURL
	}
}

// WithHTTPClient replaces the default http.DefaultClient without mutating a
// caller-owned http.Client.
func WithHTTPClient(client HTTPClient) Option {
	return func(options *ClientOptions) {
		options.httpClient = client
	}
}

// WithRequestEditorFn appends a client-level request editor. Editors run in
// registration order and must be safe for concurrent use when Client is shared.
func WithRequestEditorFn(editor RequestEditorFn) Option {
	return func(options *ClientOptions) {
		options.requestEditors = append(options.requestEditors, editor)
	}
}

// WithResponseTimeout bounds response headers and, for ordinary responses,
// reading and decoding the response body. For SSE it only bounds connection
// establishment. Zero disables the timeout.
func WithResponseTimeout(timeout time.Duration) Option {
	return func(options *ClientOptions) {
		options.responseTimeout = timeout
	}
}

// WithSSEIdleTimeout bounds silence between SSE chunks. Every received chunk,
// including a heartbeat comment, resets it. Zero disables the timeout.
func WithSSEIdleTimeout(timeout time.Duration) Option {
	return func(options *ClientOptions) {
		options.sseIdleTimeout = timeout
	}
}

// WithSSEMaxRetries bounds consecutive reconnect failures for generated GET
// event streams. Zero disables reconnect attempts; the hard cap is 100.
func WithSSEMaxRetries(retries int) Option {
	return func(options *ClientOptions) {
		options.sseMaxRetries = retries
	}
}

// WithSSEReconnectBaseDelay configures the first reconnect backoff.
func WithSSEReconnectBaseDelay(delay time.Duration) Option {
	return func(options *ClientOptions) {
		options.sseReconnectBaseDelay = delay
	}
}

// WithSSEReconnectOnStreamEnd controls whether a clean EOF reconnects a
// generated event stream. It defaults to true for compatibility with streams
// that use EOF as a transient disconnect; terminal-aware proxies can disable it.
func WithSSEReconnectOnStreamEnd(reconnect bool) Option {
	return func(options *ClientOptions) {
		options.sseReconnectOnStreamEnd = reconnect
	}
}

type Client struct {
	baseURL                 string
	uploadBaseURL           string
	httpClient              HTTPClient
	requestEditors          []RequestEditorFn
	responseTimeout         time.Duration
	sseIdleTimeout          time.Duration
	sseMaxRetries           int
	sseReconnectBaseDelay   time.Duration
	sseReconnectOnStreamEnd bool
}

func NewClient(config ClientOptions, options ...Option) (*Client, error) {
	parsed, err := url.Parse(strings.TrimSpace(config.BaseURL))
	if err != nil {
		return nil, fmt.Errorf("parse base URL %q: %w", config.BaseURL, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("base URL %q must include scheme and host", config.BaseURL)
	}
	config.httpClient = http.DefaultClient
	if strings.TrimSpace(config.UploadBaseURL) == "" {
		config.UploadBaseURL = parsed.String()
	}
	uploadParsed, err := url.Parse(strings.TrimSpace(config.UploadBaseURL))
	if err != nil {
		return nil, fmt.Errorf("parse upload base URL %q: %w", config.UploadBaseURL, err)
	}
	if uploadParsed.Scheme == "" || uploadParsed.Host == "" {
		return nil, fmt.Errorf("upload base URL %q must include scheme and host", config.UploadBaseURL)
	}
	config.responseTimeout = defaultResponseTimeout
	config.sseIdleTimeout = defaultSSEIdleTimeout
	config.sseMaxRetries = defaultSSEMaxRetries
	config.sseReconnectBaseDelay = defaultSSEReconnectBaseDelay
	config.sseReconnectOnStreamEnd = true
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("client option must not be nil")
		}
		option(&config)
	}
	if config.httpClient == nil {
		return nil, fmt.Errorf("HTTP client must not be nil")
	}
	if config.responseTimeout < 0 {
		return nil, fmt.Errorf("response timeout must not be negative")
	}
	if config.sseIdleTimeout < 0 {
		return nil, fmt.Errorf("SSE idle timeout must not be negative")
	}
	if config.sseMaxRetries < 0 || config.sseMaxRetries > maxSSERetries {
		return nil, fmt.Errorf("SSE max retries must be between 0 and %d", maxSSERetries)
	}
	if config.sseReconnectBaseDelay < 0 {
		return nil, fmt.Errorf("SSE reconnect base delay must not be negative")
	}
	for index, editor := range config.requestEditors {
		if editor == nil {
			return nil, fmt.Errorf("request editor at index %d must not be nil", index)
		}
	}
	return &Client{
		baseURL:                 strings.TrimRight(parsed.String(), "/"),
		uploadBaseURL:           strings.TrimRight(uploadParsed.String(), "/"),
		httpClient:              config.httpClient,
		requestEditors:          append([]RequestEditorFn(nil), config.requestEditors...),
		responseTimeout:         config.responseTimeout,
		sseIdleTimeout:          config.sseIdleTimeout,
		sseMaxRetries:           config.sseMaxRetries,
		sseReconnectBaseDelay:   config.sseReconnectBaseDelay,
		sseReconnectOnStreamEnd: config.sseReconnectOnStreamEnd,
	}, nil
}

func (c *Client) do(ctx context.Context, request *http.Request) (*http.Response, error) {
	for index, editor := range c.requestEditors {
		if err := editor(ctx, request); err != nil {
			return nil, fmt.Errorf("request editor at index %d: %w", index, err)
		}
	}
	// Keep caller cancellation and response timeout authoritative even if an
	// editor replaces the request value or its context.
	request = request.Clone(ctx)
	return c.httpClient.Do(request)
}

type ResponseTimeoutError struct {
	Duration time.Duration
}

func (err *ResponseTimeoutError) Error() string {
	return fmt.Sprintf("response timed out after %s", err.Duration)
}

func (err *ResponseTimeoutError) Timeout() bool { return true }

type responseLifecycle struct {
	cancel    context.CancelCauseFunc
	timer     *time.Timer
	closeOnce sync.Once
}

func (c *Client) responseContext(parent context.Context) (context.Context, *responseLifecycle) {
	ctx, cancel := context.WithCancelCause(parent)
	lifecycle := &responseLifecycle{cancel: cancel}
	if c.responseTimeout > 0 {
		lifecycle.timer = time.AfterFunc(c.responseTimeout, func() {
			cancel(&ResponseTimeoutError{Duration: c.responseTimeout})
		})
	}
	return ctx, lifecycle
}

func (lifecycle *responseLifecycle) stopTimeout() {
	if lifecycle.timer != nil {
		lifecycle.timer.Stop()
	}
}

func (lifecycle *responseLifecycle) close() {
	lifecycle.closeOnce.Do(func() {
		lifecycle.stopTimeout()
		lifecycle.cancel(context.Canceled)
	})
}

type UnexpectedStatusError struct {
	Method     string
	URL        string
	StatusCode int
	Body       string
}

func (err *UnexpectedStatusError) Error() string {
	if err.Body == "" {
		return fmt.Sprintf("%s %s returned unexpected HTTP status %d", err.Method, err.URL, err.StatusCode)
	}
	return fmt.Sprintf("%s %s returned unexpected HTTP status %d: %s", err.Method, err.URL, err.StatusCode, err.Body)
}

type sseAttempt struct {
	response  *http.Response
	lifecycle *responseLifecycle
}

func retryableSSEStatus(status int) bool {
	return status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout
}

func sseRetryDelay(base time.Duration, failure int, serverDelay *time.Duration) time.Duration {
	if serverDelay != nil {
		return *serverDelay
	}
	scale := 1.0
	for index := 1; index < failure && scale < 1<<20; index++ {
		scale *= 2
	}
	return time.Duration(float64(base) * scale * (0.9 + rand.Float64()*0.2))
}

func waitForSSERetry(ctx context.Context, delay time.Duration) error {
	if err := sseContextError(ctx); err != nil {
		return err
	}
	timer := time.NewTimer(maxDuration(delay, 0))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return sseContextError(ctx)
	case <-timer.C:
		return nil
	}
}

func sseContextError(ctx context.Context) error {
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}
	return ctx.Err()
}

func maxDuration(value time.Duration, minimum time.Duration) time.Duration {
	if value < minimum {
		return minimum
	}
	return value
}

func (c *Client) openReconnectingSSE(
	ctx context.Context,
	open func(context.Context) (*http.Response, *responseLifecycle, error),
) (*sseAttempt, error) {
	failures := 0
	for {
		if err := sseContextError(ctx); err != nil {
			return nil, err
		}
		response, lifecycle, err := open(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil, sseContextError(ctx)
			}
			if failures >= c.sseMaxRetries {
				return nil, err
			}
			failures++
			if err := waitForSSERetry(ctx, sseRetryDelay(c.sseReconnectBaseDelay, failures, nil)); err != nil {
				return nil, err
			}
			continue
		}
		attempt := &sseAttempt{response: response, lifecycle: lifecycle}
		if !retryableSSEStatus(response.StatusCode) {
			return attempt, nil
		}
		if failures >= c.sseMaxRetries {
			return attempt, nil
		}
		_ = response.Body.Close()
		lifecycle.close()
		failures++
		if err := waitForSSERetry(ctx, sseRetryDelay(c.sseReconnectBaseDelay, failures, nil)); err != nil {
			return nil, err
		}
	}
}

type reconnectingSSEBody struct {
	ctx                  context.Context
	open                 func(context.Context) (*http.Response, *responseLifecycle, error)
	maxRetries           int
	baseDelay            time.Duration
	idleTimeout          time.Duration
	reconnectOnStreamEnd bool

	mu               sync.Mutex
	current          io.ReadCloser
	currentLifecycle *responseLifecycle
	closed           bool
	terminalErr      error
	failures         int
	serverDelay      *time.Duration
	pending          []byte
	ready            [][]byte
}

func newReconnectingSSEBody(
	attempt *sseAttempt,
	ctx context.Context,
	open func(context.Context) (*http.Response, *responseLifecycle, error),
	maxRetries int,
	baseDelay time.Duration,
	idleTimeout time.Duration,
	reconnectOnStreamEnd bool,
) io.ReadCloser {
	return &reconnectingSSEBody{
		ctx:                  ctx,
		open:                 open,
		maxRetries:           maxRetries,
		baseDelay:            baseDelay,
		idleTimeout:          idleTimeout,
		reconnectOnStreamEnd: reconnectOnStreamEnd,
		current:              newSSEIdleBody(attempt.response.Body, idleTimeout),
		currentLifecycle:     attempt.lifecycle,
	}
}

func newSSEIdleBody(body io.ReadCloser, timeout time.Duration) io.ReadCloser {
	if body == nil {
		body = http.NoBody
	}
	return newIdleTimeoutBody(body, timeout, nil)
}

func (body *reconnectingSSEBody) Read(buffer []byte) (int, error) {
	if len(buffer) == 0 {
		return 0, nil
	}
	for {
		if read := body.popReady(buffer); read > 0 {
			return read, nil
		}
		body.mu.Lock()
		if body.closed {
			err := body.terminalErr
			body.mu.Unlock()
			if err == nil {
				return 0, io.EOF
			}
			return 0, err
		}
		current := body.current
		body.mu.Unlock()
		if err := sseContextError(body.ctx); err != nil {
			body.fail(err)
			return 0, err
		}
		if current == nil {
			err := fmt.Errorf("SSE response has no readable body")
			body.fail(err)
			return 0, err
		}
		chunk := make([]byte, 32<<10)
		read, err := current.Read(chunk)
		if read > 0 {
			body.push(chunk[:read])
			if ready := body.popReady(buffer); ready > 0 {
				return ready, nil
			}
		}
		if err == nil {
			continue
		}
		if err == io.EOF && !body.reconnectOnStreamEnd {
			_ = body.Close()
			return 0, io.EOF
		}
		if reconnectErr := body.reconnect(err); reconnectErr != nil {
			body.fail(reconnectErr)
			return 0, reconnectErr
		}
	}
}

func (body *reconnectingSSEBody) push(chunk []byte) {
	body.mu.Lock()
	defer body.mu.Unlock()
	body.pending = append(body.pending, chunk...)
	for {
		end := sseFrameEnd(body.pending)
		if end < 0 {
			if len(body.pending) > maxSSEEventBytes {
				body.terminalErr = fmt.Errorf("SSE event exceeds %d bytes", maxSSEEventBytes)
			}
			return
		}
		frame := append([]byte(nil), body.pending[:end]...)
		body.pending = append([]byte(nil), body.pending[end:]...)
		if retry, ok := sseRetryValue(frame); ok {
			body.serverDelay = &retry
		}
		body.failures = 0
		body.ready = append(body.ready, frame)
	}
}

func (body *reconnectingSSEBody) popReady(buffer []byte) int {
	body.mu.Lock()
	defer body.mu.Unlock()
	if len(body.ready) == 0 {
		return 0
	}
	frame := body.ready[0]
	read := copy(buffer, frame)
	if read == len(frame) {
		body.ready = body.ready[1:]
	} else {
		body.ready[0] = frame[read:]
	}
	return read
}

func (body *reconnectingSSEBody) reconnect(lastErr error) error {
	for {
		body.mu.Lock()
		if body.closed {
			err := body.terminalErr
			body.mu.Unlock()
			if err == nil {
				return io.ErrClosedPipe
			}
			return err
		}
		if body.failures >= body.maxRetries {
			body.mu.Unlock()
			return lastErr
		}
		body.failures++
		failure := body.failures
		serverDelay := body.serverDelay
		body.serverDelay = nil
		current := body.current
		lifecycle := body.currentLifecycle
		body.current = nil
		body.currentLifecycle = nil
		body.pending = nil
		body.mu.Unlock()
		if current != nil {
			_ = current.Close()
		}
		if lifecycle != nil {
			lifecycle.close()
		}
		if err := waitForSSERetry(body.ctx, sseRetryDelay(body.baseDelay, failure, serverDelay)); err != nil {
			return err
		}
		response, nextLifecycle, err := body.open(body.ctx)
		if err != nil {
			lastErr = err
			continue
		}
		if retryableSSEStatus(response.StatusCode) {
			_ = response.Body.Close()
			nextLifecycle.close()
			lastErr = fmt.Errorf("SSE reconnect returned HTTP %d", response.StatusCode)
			body.mu.Lock()
			if body.failures >= body.maxRetries {
				body.mu.Unlock()
				return lastErr
			}
			body.failures++
			failure = body.failures
			body.mu.Unlock()
			if err := waitForSSERetry(body.ctx, sseRetryDelay(body.baseDelay, failure, nil)); err != nil {
				return err
			}
			continue
		}
		if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
			_ = response.Body.Close()
			nextLifecycle.close()
			return &UnexpectedStatusError{
				Method:     response.Request.Method,
				URL:        response.Request.URL.String(),
				StatusCode: response.StatusCode,
			}
		}
		nextBody := newSSEIdleBody(response.Body, body.idleTimeout)
		body.mu.Lock()
		if body.closed {
			body.mu.Unlock()
			_ = nextBody.Close()
			nextLifecycle.close()
			return io.ErrClosedPipe
		}
		body.current = nextBody
		body.currentLifecycle = nextLifecycle
		body.mu.Unlock()
		return nil
	}
}

func (body *reconnectingSSEBody) fail(err error) {
	body.mu.Lock()
	if !body.closed {
		body.terminalErr = err
		body.closed = true
	}
	body.mu.Unlock()
}

func (body *reconnectingSSEBody) Close() error {
	body.mu.Lock()
	if body.closed {
		err := body.terminalErr
		body.mu.Unlock()
		return err
	}
	body.closed = true
	current := body.current
	lifecycle := body.currentLifecycle
	body.current = nil
	body.currentLifecycle = nil
	body.mu.Unlock()
	if current != nil {
		_ = current.Close()
	}
	if lifecycle != nil {
		lifecycle.close()
	}
	return nil
}

func sseFrameEnd(bytes []byte) int {
	for index := 1; index < len(bytes); index++ {
		if bytes[index] == '\n' && (bytes[index-1] == '\n' || (bytes[index-1] == '\r' && index >= 2 && bytes[index-2] == '\n')) {
			return index + 1
		}
	}
	return -1
}

func sseRetryValue(frame []byte) (time.Duration, bool) {
	for _, line := range strings.Split(strings.ReplaceAll(string(frame), "\r\n", "\n"), "\n") {
		if !strings.HasPrefix(line, "retry:") {
			continue
		}
		var millis int64
		if _, err := fmt.Sscanf(strings.TrimSpace(strings.TrimPrefix(line, "retry:")), "%d", &millis); err == nil && millis >= 0 {
			return time.Duration(millis) * time.Millisecond, true
		}
	}
	return 0, false
}

type WatchEventsResponse struct {
	StatusCode int
	Raw        *http.Response
	Status200  *SSEStream[Event]
}

func (c *Client) NewWatchEventsRequest(ctx context.Context) (*http.Request, error) {
	if ctx == nil {
		return nil, fmt.Errorf("build WatchEvents request: context must not be nil")
	}
	path := "/events"

	baseURL := c.baseURL
	endpoint, err := url.Parse(baseURL + path)
	if err != nil {
		return nil, fmt.Errorf("build WatchEvents URL: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build WatchEvents request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	return req, nil
}

func (c *Client) WatchEvents(ctx context.Context) (*WatchEventsResponse, error) {

	var requestForError *http.Request
	open := func(attemptCtx context.Context) (*http.Response, *responseLifecycle, error) {
		req, err := c.NewWatchEventsRequest(attemptCtx)
		if err != nil {
			return nil, nil, err
		}
		requestForError = req
		responseCtx, lifecycle := c.responseContext(attemptCtx)
		req = req.Clone(responseCtx)
		res, err := c.do(responseCtx, req)
		if err != nil {
			lifecycle.close()
			return nil, nil, fmt.Errorf("execute WatchEvents request: %w", err)
		}
		if res == nil {
			lifecycle.close()
			return nil, nil, fmt.Errorf("execute WatchEvents request: HTTP client returned nil response")
		}
		if res.Request == nil {
			res.Request = req
		}
		return res, lifecycle, nil
	}
	attempt, err := c.openReconnectingSSE(ctx, open)
	if err != nil {
		return nil, err
	}
	res := attempt.response
	lifecycle := attempt.lifecycle
	keepLifecycle := false
	defer func() {
		if !keepLifecycle {
			lifecycle.close()
		}
	}()

	result := &WatchEventsResponse{StatusCode: res.StatusCode, Raw: res}
	switch res.StatusCode {
	case 200:
		lifecycle.stopTimeout()
		keepLifecycle = true
		body := newReconnectingSSEBody(
			attempt,
			ctx,
			open,
			c.sseMaxRetries,
			c.sseReconnectBaseDelay,
			c.sseIdleTimeout,
			c.sseReconnectOnStreamEnd,
		)
		res.Body = body
		result.Status200 = newReconnectingSSEStream[Event](body, lifecycle.close)

		return result, nil
	default:
		rawBody, readErr := io.ReadAll(io.LimitReader(res.Body, maxDiagnosticBodyBytes))
		_ = res.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read unexpected WatchEvents response status %d: %w", res.StatusCode, readErr)
		}
		return nil, &UnexpectedStatusError{
			Method:     requestForError.Method,
			URL:        requestForError.URL.String(),
			StatusCode: res.StatusCode,
			Body:       strings.TrimSpace(string(rawBody)),
		}
	}
}

type CreateThingParams struct {
	ThingId    string
	Tag        *string
	Notify     bool
	Label      *[]string
	XRequestId string
	Body       CreateThing
}

type CreateThingResponse struct {
	StatusCode int
	Raw        *http.Response
	Status201  *Thing
	Status400  *Problem
}

func (c *Client) NewCreateThingRequest(ctx context.Context, params CreateThingParams) (*http.Request, error) {
	if ctx == nil {
		return nil, fmt.Errorf("build CreateThing request: context must not be nil")
	}
	if params.ThingId == "" {
		return nil, fmt.Errorf("build CreateThing request: required parameter thing_id is empty")
	}
	if params.XRequestId == "" {
		return nil, fmt.Errorf("build CreateThing request: required parameter x-request-id is empty")
	}
	path := "/things/{thing_id}"

	path = strings.ReplaceAll(path, "{thing_id}", url.PathEscape(fmt.Sprint(params.ThingId)))
	baseURL := c.baseURL
	endpoint, err := url.Parse(baseURL + path)
	if err != nil {
		return nil, fmt.Errorf("build CreateThing URL: %w", err)
	}
	query := endpoint.Query()
	if params.Tag != nil {
		query.Set("tag", fmt.Sprint(*params.Tag))
	}
	query.Set("notify", fmt.Sprint(params.Notify))
	if params.Label != nil {
		for _, value := range *params.Label {
			query.Add("label", fmt.Sprint(value))
		}
	}
	endpoint.RawQuery = query.Encode()
	var requestBody io.Reader
	encodedBody, err := json.Marshal(params.Body)
	if err != nil {
		return nil, fmt.Errorf("encode CreateThing JSON body: %w", err)
	}
	requestBody = bytes.NewReader(encodedBody)
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint.String(), requestBody)
	if err != nil {
		return nil, fmt.Errorf("build CreateThing request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if requestBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("x-request-id", fmt.Sprint(params.XRequestId))
	return req, nil
}

func (c *Client) CreateThing(ctx context.Context, params CreateThingParams) (*CreateThingResponse, error) {

	req, err := c.NewCreateThingRequest(ctx, params)
	if err != nil {
		return nil, err
	}
	responseCtx, lifecycle := c.responseContext(ctx)
	req = req.Clone(responseCtx)
	keepLifecycle := false
	defer func() {
		if !keepLifecycle {
			lifecycle.close()
		}
	}()
	res, err := c.do(responseCtx, req)
	if err != nil {
		return nil, fmt.Errorf("execute CreateThing request: %w", err)
	}
	if res == nil {
		return nil, fmt.Errorf("execute CreateThing request: HTTP client returned nil response")
	}

	result := &CreateThingResponse{StatusCode: res.StatusCode, Raw: res}
	switch res.StatusCode {
	case 201:
		var decoded Thing
		if err := json.NewDecoder(io.LimitReader(res.Body, maxDecodedBodyBytes)).Decode(&decoded); err != nil {
			_ = res.Body.Close()
			return nil, fmt.Errorf("decode CreateThing status 201 response: %w", err)
		}
		_ = res.Body.Close()
		result.Status201 = &decoded
		return result, nil
	case 400:
		var decoded Problem
		if err := json.NewDecoder(io.LimitReader(res.Body, maxDecodedBodyBytes)).Decode(&decoded); err != nil {
			_ = res.Body.Close()
			return nil, fmt.Errorf("decode CreateThing status 400 response: %w", err)
		}
		_ = res.Body.Close()
		result.Status400 = &decoded
		return result, nil
	default:
		rawBody, readErr := io.ReadAll(io.LimitReader(res.Body, maxDiagnosticBodyBytes))
		_ = res.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read unexpected CreateThing response status %d: %w", res.StatusCode, readErr)
		}
		return nil, &UnexpectedStatusError{
			Method:     req.Method,
			URL:        req.URL.String(),
			StatusCode: res.StatusCode,
			Body:       strings.TrimSpace(string(rawBody)),
		}
	}
}

type UploadMediaParams struct {
	Owner       string
	UploadType  string
	Body        io.Reader
	ContentType string
}

type UploadMediaResponse struct {
	StatusCode int
	Raw        *http.Response
	Status201  *Thing
}

func (c *Client) NewUploadMediaRequest(ctx context.Context, params UploadMediaParams) (*http.Request, error) {
	if ctx == nil {
		return nil, fmt.Errorf("build UploadMedia request: context must not be nil")
	}
	if params.Owner == "" {
		return nil, fmt.Errorf("build UploadMedia request: required parameter owner is empty")
	}
	if params.UploadType == "" {
		return nil, fmt.Errorf("build UploadMedia request: required parameter uploadType is empty")
	}
	path := "/uploads/{owner}"
	if params.Body == nil {
		return nil, fmt.Errorf("build UploadMedia request: required request body is nil")
	}

	path = strings.ReplaceAll(path, "{owner}", url.PathEscape(fmt.Sprint(params.Owner)))
	baseURL := c.baseURL
	endpoint, err := url.Parse(baseURL + path)
	if err != nil {
		return nil, fmt.Errorf("build UploadMedia URL: %w", err)
	}
	query := endpoint.Query()
	query.Set("uploadType", fmt.Sprint(params.UploadType))
	endpoint.RawQuery = query.Encode()
	requestBody := params.Body
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint.String(), requestBody)
	if err != nil {
		return nil, fmt.Errorf("build UploadMedia request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if requestBody != nil {
		contentType := strings.TrimSpace(params.ContentType)
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		req.Header.Set("Content-Type", contentType)
	}
	return req, nil
}

func (c *Client) UploadMedia(ctx context.Context, params UploadMediaParams) (*UploadMediaResponse, error) {

	req, err := c.NewUploadMediaRequest(ctx, params)
	if err != nil {
		return nil, err
	}
	responseCtx, lifecycle := c.responseContext(ctx)
	req = req.Clone(responseCtx)
	keepLifecycle := false
	defer func() {
		if !keepLifecycle {
			lifecycle.close()
		}
	}()
	res, err := c.do(responseCtx, req)
	if err != nil {
		return nil, fmt.Errorf("execute UploadMedia request: %w", err)
	}
	if res == nil {
		return nil, fmt.Errorf("execute UploadMedia request: HTTP client returned nil response")
	}

	result := &UploadMediaResponse{StatusCode: res.StatusCode, Raw: res}
	switch res.StatusCode {
	case 201:
		var decoded Thing
		if err := json.NewDecoder(io.LimitReader(res.Body, maxDecodedBodyBytes)).Decode(&decoded); err != nil {
			_ = res.Body.Close()
			return nil, fmt.Errorf("decode UploadMedia status 201 response: %w", err)
		}
		_ = res.Body.Close()
		result.Status201 = &decoded
		return result, nil
	default:
		rawBody, readErr := io.ReadAll(io.LimitReader(res.Body, maxDiagnosticBodyBytes))
		_ = res.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read unexpected UploadMedia response status %d: %w", res.StatusCode, readErr)
		}
		return nil, &UnexpectedStatusError{
			Method:     req.Method,
			URL:        req.URL.String(),
			StatusCode: res.StatusCode,
			Body:       strings.TrimSpace(string(rawBody)),
		}
	}
}

type SSEIdleTimeoutError struct {
	Duration time.Duration
}

func (err *SSEIdleTimeoutError) Error() string {
	return fmt.Sprintf("SSE stream was idle for %s", err.Duration)
}

func (err *SSEIdleTimeoutError) Timeout() bool { return true }

type idleTimeoutBody struct {
	body      io.ReadCloser
	duration  time.Duration
	onTimeout func()

	mu        sync.Mutex
	timer     *time.Timer
	timedOut  bool
	closed    bool
	closeOnce sync.Once
	closeErr  error
}

func newIdleTimeoutBody(body io.ReadCloser, duration time.Duration, onTimeout func()) io.ReadCloser {
	if duration == 0 {
		return body
	}
	wrapped := &idleTimeoutBody{
		body:      body,
		duration:  duration,
		onTimeout: onTimeout,
	}
	wrapped.timer = time.AfterFunc(duration, wrapped.expire)
	return wrapped
}

func (body *idleTimeoutBody) expire() {
	body.mu.Lock()
	if body.closed {
		body.mu.Unlock()
		return
	}
	body.timedOut = true
	body.mu.Unlock()
	if body.onTimeout != nil {
		body.onTimeout()
	}
	_ = body.body.Close()
}

func (body *idleTimeoutBody) Read(buffer []byte) (int, error) {
	read, err := body.body.Read(buffer)
	body.mu.Lock()
	if read > 0 && !body.timedOut && !body.closed {
		body.timer.Reset(body.duration)
	}
	timedOut := body.timedOut
	body.mu.Unlock()
	if timedOut && err != nil {
		return read, &SSEIdleTimeoutError{Duration: body.duration}
	}
	return read, err
}

func (body *idleTimeoutBody) Close() error {
	body.closeOnce.Do(func() {
		body.mu.Lock()
		body.closed = true
		body.timer.Stop()
		body.mu.Unlock()
		body.closeErr = body.body.Close()
	})
	return body.closeErr
}

type SSEStream[T any] struct {
	body         io.ReadCloser
	scanner      *bufio.Scanner
	closeOnce    sync.Once
	nextMu       sync.Mutex
	data         []string
	currentEvent string
	lastEvent    string
	finished     bool
	closeErr     error
	cleanup      func()
}

func NewSSEStream[T any](body io.ReadCloser) *SSEStream[T] {
	return newSSEStream[T](body, defaultSSEIdleTimeout, nil)
}

func newSSEStream[T any](body io.ReadCloser, idleTimeout time.Duration, cleanup func()) *SSEStream[T] {
	body = newIdleTimeoutBody(body, idleTimeout, cleanup)
	return newSSEStreamWithoutIdle[T](body, cleanup)
}

func newReconnectingSSEStream[T any](body io.ReadCloser, cleanup func()) *SSEStream[T] {
	return newSSEStreamWithoutIdle[T](body, cleanup)
}

func newSSEStreamWithoutIdle[T any](body io.ReadCloser, cleanup func()) *SSEStream[T] {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64<<10), maxSSEEventBytes)
	return &SSEStream[T]{body: body, scanner: scanner, cleanup: cleanup}
}

// EventName returns the event field associated with the item most recently
// returned by Next. An omitted event field produces an empty string.
func (stream *SSEStream[T]) EventName() string {
	stream.nextMu.Lock()
	defer stream.nextMu.Unlock()
	return stream.lastEvent
}

// Next returns one decoded SSE data payload without buffering the full stream.
// The boolean is false after clean EOF. Close ends consumption early.
func (stream *SSEStream[T]) Next(ctx context.Context) (T, bool, error) {
	stream.nextMu.Lock()
	defer stream.nextMu.Unlock()

	var zero T
	if stream.finished {
		return zero, false, nil
	}
	if err := ctx.Err(); err != nil {
		_ = stream.Close()
		return zero, false, err
	}
	wake := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = stream.Close()
		case <-wake:
		}
	}()
	defer close(wake)

	for stream.scanner.Scan() {
		line := stream.scanner.Text()
		if line == "" {
			decoded, ok, err := stream.decodeEvent()
			if ok || err != nil {
				return decoded, ok, err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, found := strings.Cut(line, ":")
		if !found {
			value = ""
		}
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "data":
			stream.data = append(stream.data, value)
		case "event":
			stream.currentEvent = value
		}
	}
	if err := ctx.Err(); err != nil {
		stream.finished = true
		_ = stream.Close()
		return zero, false, err
	}
	if err := stream.scanner.Err(); err != nil {
		stream.finished = true
		_ = stream.Close()
		return zero, false, fmt.Errorf("read SSE stream: %w", err)
	}
	stream.finished = true
	decoded, ok, err := stream.decodeEvent()
	_ = stream.Close()
	return decoded, ok, err
}

func (stream *SSEStream[T]) decodeEvent() (T, bool, error) {
	var zero T
	payload := strings.Join(stream.data, "\n")
	stream.data = nil
	stream.lastEvent = stream.currentEvent
	stream.currentEvent = ""
	if strings.TrimSpace(payload) == "" {
		return zero, false, nil
	}
	var decoded T
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		stream.finished = true
		_ = stream.Close()
		return zero, false, fmt.Errorf("decode SSE JSON event: %w", err)
	}
	return decoded, true, nil
}

func (stream *SSEStream[T]) Close() error {
	stream.closeOnce.Do(func() {
		stream.closeErr = stream.body.Close()
		if stream.cleanup != nil {
			stream.cleanup()
		}
	})
	return stream.closeErr
}
