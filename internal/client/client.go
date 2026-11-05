// Package client is the API layer: everything touching HTTP, URLs or JSON lives
// here, and it never imports a Terraform package. Resources call these methods
// instead of building requests, so the hard parts — rate limiting, retries,
// idempotency keys, the response envelope — are written once and shared.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-log/tflog"
	"golang.org/x/time/rate"
)

const (
	// defaultBaseURL is the production API, used when neither the provider config
	// nor the environment overrides it.
	defaultBaseURL = "https://api.strettch.cloud/api/v1"

	// apiRateLimit is the API's hard per-key ceiling (2 req/s); the configured
	// rate is capped to it.
	apiRateLimit = 2.0

	// perRequestTimeout bounds a single HTTP attempt. Long waits belong to the
	// poll loop, not one request.
	perRequestTimeout = 30 * time.Second

	// maxRateResetWait limits how long we honor the server's X-RateLimit-Reset.
	// Unreasonably long waits (e.g. invalid timestamps or clock skew) return an
	// error instead of blocking requests for hours.
	maxRateResetWait = 60 * time.Second

	headerRequestSource = "X-Request-Source"
	headerIdempotency   = "Idempotency-Key"
	headerRateReset     = "X-RateLimit-Reset"
)

// Config holds everything needed to build a Client. Configure() fills it from
// provider config, env fallbacks and defaults.
type Config struct {
	APIKey            string
	BaseURL           string
	UserAgent         string
	RequestsPerSecond float64
	RetryMax          int
	RetryWaitMin      time.Duration
	RetryWaitMax      time.Duration

	// HTTPClient is optional; tests inject one pointed at an httptest server.
	HTTPClient *http.Client
}

// Client is the single API client shared by every resource and data source.
// Configure() builds it once per Terraform command and hands it out via
// ProviderData.
type Client struct {
	httpClient   *http.Client
	baseURL      string
	apiKey       string
	userAgent    string
	limiter      *rate.Limiter
	retryMax     int
	retryWaitMin time.Duration
	retryWaitMax time.Duration

	// pollScheduleFn returns the wait before the i-th poll. It is a field so
	// tests can shrink the schedule; production uses defaultPollSchedule
	// (2s, 4s, 8s, then 10s).
	pollScheduleFn func(i int) time.Duration
}

// NewClient builds a Client from Config, applying the defaults and the rate cap.
func NewClient(cfg Config) *Client {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	rps := cfg.RequestsPerSecond
	if rps <= 0 {
		rps = apiRateLimit
	}
	if rps > apiRateLimit {
		rps = apiRateLimit // values above the API limit are capped
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: perRequestTimeout}
	}

	retryMax := cfg.RetryMax
	if retryMax < 0 {
		retryMax = 0 // a negative count would skip the loop and leave lastErr nil
	}

	return &Client{
		httpClient:     httpClient,
		baseURL:        baseURL,
		apiKey:         cfg.APIKey,
		userAgent:      cfg.userAgentOrDefault(),
		limiter:        rate.NewLimiter(rate.Limit(rps), 1),
		retryMax:       retryMax,
		retryWaitMin:   cfg.RetryWaitMin,
		retryWaitMax:   cfg.RetryWaitMax,
		pollScheduleFn: defaultPollSchedule,
	}
}

func (cfg Config) userAgentOrDefault() string {
	if cfg.UserAgent != "" {
		return cfg.UserAgent
	}
	return "terraform-provider-strettchcloud"
}

// envelope wraps every response: data (plus meta for lists) on success,
// code/message on error. success:false or a non-2xx status becomes an APIError.
type envelope struct {
	Success   bool            `json:"success"`
	Message   string          `json:"message"`
	Code      string          `json:"code"`
	Data      json.RawMessage `json:"data"`
	RequestID string          `json:"requestId"`
	Meta      *PaginationMeta `json:"meta"`
}

// PaginationMeta mirrors the API's list metadata.
type PaginationMeta struct {
	Total int `json:"total"`
	Page  int `json:"page"`
	Limit int `json:"limit"`
}

// requestOptions tune a single call.
type requestOptions struct {
	// idempotencyKey, when set, is sent as Idempotency-Key and reused across this
	// request's retries, so a replay returns the original response.
	idempotencyKey string
	// query is appended to the URL as query parameters.
	query url.Values
}

// attemptResult carries what do() needs from a single HTTP attempt to decide
// whether and how long to wait before retrying.
type attemptResult struct {
	meta      *PaginationMeta
	retry     bool
	resetWait time.Duration // from X-RateLimit-Reset on a 429, else 0
}

// do performs one API call end to end: rate limit, send (with retries), unwrap
// the envelope, and decode data into out. It returns the pagination meta when
// present. out may be nil when the caller does not need the body.
func (c *Client) do(ctx context.Context, method, path string, body, out any, opts requestOptions) (*PaginationMeta, error) {
	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encoding request body: %w", err)
		}
	}

	fullURL := c.baseURL + path
	if len(opts.query) > 0 {
		fullURL += "?" + opts.query.Encode()
	}

	var lastErr error
	// rateResetWait carries a server-requested X-RateLimit-Reset wait to the next
	// attempt, where it replaces (rather than adds to) the exponential backoff.
	var rateResetWait time.Duration
	// One initial attempt plus retryMax retries.
	for attempt := 0; attempt <= c.retryMax; attempt++ {
		if attempt > 0 {
			wait := c.backoff(attempt)
			if rateResetWait > 0 {
				wait = rateResetWait
				rateResetWait = 0
			}
			tflog.Debug(ctx, "retrying request", map[string]any{
				"attempt": attempt,
				"reason":  lastErr.Error(),
				"wait":    wait.String(),
			})
			if err := sleep(ctx, wait); err != nil {
				return nil, err
			}
		}

		res, err := c.attempt(ctx, method, fullURL, path, bodyBytes, out, opts)
		if err == nil {
			return res.meta, nil
		}
		lastErr = err
		if !res.retry {
			return nil, err
		}
		if res.resetWait > maxRateResetWait {
			return nil, fmt.Errorf("server requested a rate-limit wait of %s, exceeding the %s ceiling: %w",
				res.resetWait.Round(time.Second), maxRateResetWait, lastErr)
		}
		if res.resetWait > 0 {
			tflog.Warn(ctx, "rate limited; will wait for reset before retrying", map[string]any{"reset": res.resetWait.String()})
			rateResetWait = res.resetWait
		}
	}

	tflog.Error(ctx, "request failed after retries", map[string]any{"error": lastErr.Error()})
	return nil, lastErr
}

// attempt makes a single HTTP request and reports the outcome.
func (c *Client) attempt(ctx context.Context, method, fullURL, path string, bodyBytes []byte, out any, opts requestOptions) (attemptResult, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return attemptResult{}, err
	}

	reqCtx, cancel := context.WithTimeout(ctx, perRequestTimeout)
	defer cancel()

	var reqBody io.Reader
	if bodyBytes != nil {
		reqBody = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(reqCtx, method, fullURL, reqBody)
	if err != nil {
		return attemptResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set(headerRequestSource, "terraform")
	req.Header.Set("Accept", "application/json")
	if bodyBytes != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if opts.idempotencyKey != "" {
		req.Header.Set(headerIdempotency, opts.idempotencyKey)
	}

	start := time.Now()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Network/timeout errors are retryable.
		return attemptResult{retry: true}, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBytes, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return attemptResult{retry: true}, fmt.Errorf("reading response body: %w", readErr)
	}

	tflog.Debug(ctx, "api request", map[string]any{
		"method":    method,
		"path":      path,
		"status":    resp.StatusCode,
		"requestId": requestIDFromBody(respBytes),
		"duration":  time.Since(start).String(),
	})

	if resp.StatusCode >= 400 {
		apiErr := parseAPIError(resp, respBytes)
		res := attemptResult{retry: isRetryable(resp.StatusCode)}
		if resp.StatusCode == http.StatusTooManyRequests {
			res.resetWait = parseRateReset(resp.Header.Get(headerRateReset))
		}
		return res, apiErr
	}

	var env envelope
	if len(respBytes) > 0 {
		if err := json.Unmarshal(respBytes, &env); err != nil {
			return attemptResult{}, fmt.Errorf("decoding response envelope: %w", err)
		}
	}
	if !env.Success {
		return attemptResult{}, &APIError{
			Status:    resp.StatusCode,
			Code:      firstNonEmpty(env.Code, "UNKNOWN"),
			Message:   firstNonEmpty(env.Message, "request did not succeed"),
			RequestID: env.RequestID,
		}
	}

	if out != nil && len(env.Data) > 0 {
		if err := json.Unmarshal(env.Data, out); err != nil {
			return attemptResult{}, fmt.Errorf("decoding response data: %w", err)
		}
	}
	return attemptResult{meta: env.Meta}, nil
}

// backoff returns the wait before the given retry attempt (1-based),
// exponential from retryWaitMin, capped at retryWaitMax.
func (c *Client) backoff(attempt int) time.Duration {
	if c.retryWaitMin <= 0 {
		return c.retryWaitMax
	}
	wait := c.retryWaitMin << (attempt - 1)
	if wait > c.retryWaitMax || wait <= 0 {
		wait = c.retryWaitMax
	}
	return wait
}

// parseAPIError builds an APIError from a non-2xx response, preferring the
// error envelope and falling back to status text.
func parseAPIError(resp *http.Response, body []byte) *APIError {
	var env envelope
	_ = json.Unmarshal(body, &env)
	code := env.Code
	if code == "" {
		code = strings.ReplaceAll(strings.ToUpper(http.StatusText(resp.StatusCode)), " ", "_")
	}
	msg := env.Message
	if msg == "" {
		msg = http.StatusText(resp.StatusCode)
	}
	return &APIError{
		Status:    resp.StatusCode,
		Code:      code,
		Message:   msg,
		RequestID: env.RequestID,
	}
}

// parseRateReset turns an X-RateLimit-Reset header into a wait duration. The
// value may be seconds-to-wait or a unix epoch; large values are treated as
// epoch seconds.
func parseRateReset(h string) time.Duration {
	h = strings.TrimSpace(h)
	if h == "" {
		return 0
	}
	n, err := strconv.ParseInt(h, 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	if n > 1_000_000_000 { // looks like a unix timestamp
		d := time.Until(time.Unix(n, 0))
		if d < 0 {
			return 0
		}
		return d
	}
	return time.Duration(n) * time.Second
}

// sleep waits for d or until ctx is cancelled, whichever comes first.
func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func requestIDFromBody(body []byte) string {
	var env envelope
	_ = json.Unmarshal(body, &env)
	return env.RequestID
}

// defaultPollSchedule yields the wait before each poll: 2s, 4s, 8s, then every
// 10s.
func defaultPollSchedule(i int) time.Duration {
	switch i {
	case 0:
		return 2 * time.Second
	case 1:
		return 4 * time.Second
	case 2:
		return 8 * time.Second
	default:
		return 10 * time.Second
	}
}
