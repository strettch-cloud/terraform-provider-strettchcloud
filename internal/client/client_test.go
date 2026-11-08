package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// errBodyTransport returns a 200 response whose body fails on read, to exercise
// the response-body read-error branch.
type errBodyTransport struct{}

func (errBodyTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(errReader{}),
		Header:     make(http.Header),
	}, nil
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, fmt.Errorf("boom reading body") }

func TestAttempt_BodyReadError(t *testing.T) {
	c := NewClient(Config{
		APIKey:       "k",
		BaseURL:      "http://example.invalid",
		RetryMax:     1,
		RetryWaitMin: time.Millisecond,
		RetryWaitMax: time.Millisecond,
		HTTPClient:   &http.Client{Transport: errBodyTransport{}},
	})
	if _, err := c.GetCompute(context.Background(), "1"); err == nil {
		t.Fatal("want body read error")
	}
}

// newTestClient builds a Client pointed at the given test server with fast,
// deterministic retry/poll timing.
func newTestClient(serverURL string) *Client {
	c := NewClient(Config{
		APIKey:            "test-key",
		BaseURL:           serverURL,
		UserAgent:         "test-agent",
		RequestsPerSecond: apiRateLimit,
		RetryMax:          4,
		RetryWaitMin:      time.Millisecond,
		RetryWaitMax:      5 * time.Millisecond,
		HTTPClient:        &http.Client{Timeout: perRequestTimeout},
	})
	// Fast polls so WaitForState tests don't sleep for real seconds.
	c.pollScheduleFn = func(int) time.Duration { return time.Millisecond }
	return c
}

// writeEnvelope writes a success envelope wrapping data.
func writeEnvelope(w http.ResponseWriter, status int, data any, meta *PaginationMeta) {
	raw, _ := json.Marshal(data)
	env := map[string]any{
		"success":   true,
		"message":   "ok",
		"data":      json.RawMessage(raw),
		"requestId": "req-123",
	}
	if meta != nil {
		env["meta"] = meta
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(env)
}

// writeError writes an error envelope.
func writeError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success":   false,
		"code":      code,
		"message":   msg,
		"requestId": "req-err",
	})
}

func TestCreateCompute_Success(t *testing.T) {
	var gotIdempotency, gotSource, gotAuth, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIdempotency = r.Header.Get(headerIdempotency)
		gotSource = r.Header.Get(headerRequestSource)
		gotAuth = r.Header.Get("Authorization")
		gotUA = r.Header.Get("User-Agent")
		writeEnvelope(w, http.StatusAccepted, Compute{ID: "325", State: StateStarting, HostName: "web-01"}, nil)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	got, err := c.CreateCompute(context.Background(), ComputeCreateRequest{HostName: "web-01"}, "idem-key-1")
	if err != nil {
		t.Fatalf("CreateCompute: %v", err)
	}
	if got.ID != "325" || got.State != StateStarting {
		t.Fatalf("unexpected compute: %+v", got)
	}
	if gotIdempotency != "idem-key-1" {
		t.Errorf("Idempotency-Key = %q, want idem-key-1", gotIdempotency)
	}
	if gotSource != "terraform" {
		t.Errorf("X-Request-Source = %q, want terraform", gotSource)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotUA != "test-agent" {
		t.Errorf("User-Agent = %q", gotUA)
	}
}

func TestDo_RetriesOn429ThenSucceeds(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n == 1 {
			w.Header().Set(headerRateReset, "0")
			writeError(w, http.StatusTooManyRequests, "TOO_MANY_REQUESTS", "slow down")
			return
		}
		writeEnvelope(w, http.StatusOK, Compute{ID: "1", State: StateRunning}, nil)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	got, err := c.GetCompute(context.Background(), "1")
	if err != nil {
		t.Fatalf("GetCompute: %v", err)
	}
	if got.State != StateRunning {
		t.Fatalf("state = %q", got.State)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func TestDo_NoRetryOn401(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "bad key")
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.GetCompute(context.Background(), "1")
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *APIError
	if !asAPIError(err, &apiErr) || apiErr.Status != http.StatusUnauthorized {
		t.Fatalf("want 401 APIError, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 (no retry on 401)", calls)
	}
}

func TestDo_IdempotencyKeyReusedAcrossRetries(t *testing.T) {
	var mu sync.Mutex
	var keys []string
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		n := calls
		keys = append(keys, r.Header.Get(headerIdempotency))
		mu.Unlock()
		if n <= 2 {
			// Simulate a dropped connection by hijacking and closing.
			hj, ok := w.(http.Hijacker)
			if ok {
				conn, _, _ := hj.Hijack()
				_ = conn.Close()
				return
			}
		}
		writeEnvelope(w, http.StatusAccepted, Compute{ID: "9"}, nil)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.CreateCompute(context.Background(), ComputeCreateRequest{HostName: "x"}, "stable-key")
	if err != nil {
		t.Fatalf("CreateCompute: %v", err)
	}
	if calls < 3 {
		t.Fatalf("calls = %d, want >= 3", calls)
	}
	for i, k := range keys {
		if k != "stable-key" {
			t.Fatalf("attempt %d used key %q, want stable-key (reused across retries)", i, k)
		}
	}
}

func TestListAllComputes_Paginates(t *testing.T) {
	// 3 pages of 100, 100, 50 => total 250.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		var items []Compute
		switch page {
		case "1", "2":
			items = makeComputes(100)
		case "3":
			items = makeComputes(50)
		}
		writeEnvelope(w, http.StatusOK, items, &PaginationMeta{Total: 250, Page: 1, Limit: 100})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	all, err := c.ListAllComputes(context.Background())
	if err != nil {
		t.Fatalf("ListAllComputes: %v", err)
	}
	if len(all) != 250 {
		t.Fatalf("got %d computes, want 250", len(all))
	}
}

func TestWaitForState_StartingThenRunning(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		state := StateStarting
		if n >= 3 {
			state = StateRunning
		}
		writeEnvelope(w, http.StatusOK, Compute{ID: "1", State: state}, nil)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	// newTestClient already shortens the poll schedule. Give the context an ample
	// deadline and let the handler flip to RUNNING by the 3rd poll.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	got, err := c.WaitForState(ctx, "1", "", StateRunning)
	if err != nil {
		t.Fatalf("WaitForState: %v", err)
	}
	if got.State != StateRunning {
		t.Fatalf("state = %q", got.State)
	}
}

func TestWaitForState_ToleratesFromState(t *testing.T) {
	// After a stop, the first poll can still see the pre-action RUNNING before the
	// API applies the transition. WaitForState must tolerate `from` and keep
	// polling until the target STOPPED, not report "reached RUNNING".
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		state := StateRunning // pre-action state still showing on the first poll
		if n >= 2 {
			state = StateStopped
		}
		writeEnvelope(w, http.StatusOK, Compute{ID: "1", State: state}, nil)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	got, err := c.WaitForState(context.Background(), "1", StateRunning, StateStopped)
	if err != nil {
		t.Fatalf("WaitForState: %v", err)
	}
	if got.State != StateStopped {
		t.Fatalf("state = %q, want STOPPED", got.State)
	}
}

func TestWaitForSpecification(t *testing.T) {
	// After an upsize the state settles back to STOPPED, so the specification is
	// the signal: the first poll shows the old spec (resize not applied yet), the
	// next the new one.
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		spec := "S-1C-1G"
		if n >= 2 {
			spec = "S-2C-4G"
		}
		writeEnvelope(w, http.StatusOK, Compute{ID: "1", State: StateStopped, Specification: spec}, nil)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	got, err := c.WaitForSpecification(context.Background(), "1", "S-2C-4G")
	if err != nil {
		t.Fatalf("WaitForSpecification: %v", err)
	}
	if got.Specification != "S-2C-4G" {
		t.Fatalf("spec = %q, want S-2C-4G", got.Specification)
	}
}

func TestWaitForReboot(t *testing.T) {
	// The pre-reboot state is RUNNING; updatedAt distinguishes the post-reboot
	// RUNNING. First poll returns the pre-reboot RUNNING (same updatedAt), the
	// next the rebooted one.
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		updated := "t1" // unchanged from prior on the first poll
		if n >= 2 {
			updated = "t2"
		}
		writeEnvelope(w, http.StatusOK, Compute{ID: "1", State: StateRunning, UpdatedAt: updated}, nil)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	got, err := c.WaitForReboot(context.Background(), "1", "t1")
	if err != nil {
		t.Fatalf("WaitForReboot: %v", err)
	}
	if got.UpdatedAt != "t2" {
		t.Fatalf("updatedAt = %q, want t2 (rebooted)", got.UpdatedAt)
	}
}

func TestWaitForReboot_UnexpectedStateIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeEnvelope(w, http.StatusOK, Compute{ID: "1", State: StateDestroyed}, nil)
	}))
	defer srv.Close()
	c := newTestClient(srv.URL)
	if _, err := c.WaitForReboot(context.Background(), "1", "t1"); err == nil {
		t.Fatal("DESTROYED during reboot should error")
	}
}

func TestWaitForSpecification_GetErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "no")
	}))
	defer srv.Close()
	c := newTestClient(srv.URL)
	if _, err := c.WaitForSpecification(context.Background(), "1", "S-2C-4G"); err == nil {
		t.Fatal("want propagated error")
	}
}

func TestWaitForSpecification_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeEnvelope(w, http.StatusOK, Compute{ID: "1", State: StateStopped, Specification: "S-1C-1G"}, nil) // never resizes
	}))
	defer srv.Close()
	c := newTestClient(srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.WaitForSpecification(ctx, "1", "S-2C-4G"); err == nil {
		t.Fatal("want timeout error")
	}
}

func TestWaitForReboot_GetErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "no")
	}))
	defer srv.Close()
	c := newTestClient(srv.URL)
	if _, err := c.WaitForReboot(context.Background(), "1", "t1"); err == nil {
		t.Fatal("want propagated error")
	}
}

func TestWaitForReboot_Timeout(t *testing.T) {
	// A transitional REBOOTING state is skipped (never settles), so the wait runs
	// out the cancelled context — covering both the transitional-continue and the
	// timeout branches.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeEnvelope(w, http.StatusOK, Compute{ID: "1", State: StateRebooting}, nil)
	}))
	defer srv.Close()
	c := newTestClient(srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.WaitForReboot(ctx, "1", "t1"); err == nil {
		t.Fatal("want timeout error")
	}
}

func TestNewClient_NegativeRetryMaxClamped(t *testing.T) {
	// A negative RetryMax is clamped to 0 rather than skipping the retry loop and
	// leaving a nil error.
	c := NewClient(Config{APIKey: "k", BaseURL: "http://x", RetryMax: -3})
	if c == nil {
		t.Fatal("NewClient returned nil")
	}
}

func TestWaitForRunning_ToleratesInitialStopped(t *testing.T) {
	// The API creates a compute STOPPED, then auto-starts it. WaitForRunning must
	// treat the initial STOPPED as transient and only settle on RUNNING.
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		state := StateStopped
		if n >= 3 {
			state = StateRunning
		}
		writeEnvelope(w, http.StatusOK, Compute{ID: "1", State: state}, nil)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	got, err := c.WaitForRunning(context.Background(), "1")
	if err != nil {
		t.Fatalf("WaitForRunning: %v", err)
	}
	if got.State != StateRunning {
		t.Fatalf("state = %q, want RUNNING", got.State)
	}
}

func TestWaitForRunning_DestroyedIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeEnvelope(w, http.StatusOK, Compute{ID: "1", State: StateDestroyed}, nil)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	if _, err := c.WaitForRunning(context.Background(), "1"); err == nil {
		t.Fatal("expected error when compute reaches DESTROYED before RUNNING")
	}
}

func TestWaitForState_WrongStableState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeEnvelope(w, http.StatusOK, Compute{ID: "1", State: StateStopped}, nil)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.WaitForState(context.Background(), "1", "", StateRunning)
	if err == nil {
		t.Fatal("expected error for wrong stable state")
	}
}

func TestDeleteCompute_404IsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "RESOURCE_NOT_FOUND", "gone")
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	if err := c.DeleteCompute(context.Background(), "1"); err != nil {
		t.Fatalf("DeleteCompute should treat 404 as success, got %v", err)
	}
}

func TestDefaultPollSchedule(t *testing.T) {
	want := []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second, 10 * time.Second, 10 * time.Second}
	for i, w := range want {
		if got := defaultPollSchedule(i); got != w {
			t.Errorf("defaultPollSchedule(%d) = %v, want %v", i, got, w)
		}
	}
}

func TestComputeMethods_ErrorPaths(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "nope")
	}))
	defer srv.Close()
	c := newTestClient(srv.URL)
	ctx := context.Background()

	if _, err := c.CreateCompute(ctx, ComputeCreateRequest{}, "k"); err == nil {
		t.Error("CreateCompute should error")
	}
	if _, err := c.GetCompute(ctx, "1"); err == nil {
		t.Error("GetCompute should error")
	}
	host := "h"
	if _, err := c.UpdateCompute(ctx, "1", ComputeUpdateRequest{HostName: &host}); err == nil {
		t.Error("UpdateCompute should error")
	}
	if _, err := c.UpsizeCompute(ctx, "1", "S-2C-4G"); err == nil {
		t.Error("UpsizeCompute should error")
	}
	if _, err := c.StartCompute(ctx, "1"); err == nil {
		t.Error("StartCompute should error")
	}
}

func TestParseRateReset_FutureEpoch(t *testing.T) {
	future := time.Now().Add(2 * time.Hour).Unix()
	got := parseRateReset(strconv.FormatInt(future, 10))
	if got <= 0 || got > 2*time.Hour+time.Minute {
		t.Errorf("future epoch should yield a positive wait near 2h, got %v", got)
	}
}

func TestDo_ResetWaitCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(headerRateReset, "30") // within the ceiling, so we sleep
		writeError(w, http.StatusTooManyRequests, "TOO_MANY_REQUESTS", "slow")
	}))
	defer srv.Close()
	c := newTestClient(srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := c.GetCompute(ctx, "1"); err == nil {
		t.Fatal("want context error while waiting for reset")
	}
}

func TestDo_ResetWaitExceedsCeiling(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set(headerRateReset, "3600") // beyond the ceiling
		writeError(w, http.StatusTooManyRequests, "TOO_MANY_REQUESTS", "slow")
	}))
	defer srv.Close()
	c := newTestClient(srv.URL)
	// No deadline: a pre-cap client would have slept for an hour. The ceiling
	// turns an over-long reset into an immediate error instead.
	_, err := c.GetCompute(context.Background(), "1")
	if err == nil {
		t.Fatal("want error when the server reset exceeds the ceiling")
	}
	if !strings.Contains(err.Error(), "exceeding") {
		t.Errorf("want a ceiling error, got %v", err)
	}
	if calls != 1 {
		t.Errorf("want exactly one attempt before the ceiling error, got %d", calls)
	}
}

func TestAttempt_LimiterWaitCancelledCtx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	c := newTestClient(srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // limiter.Wait returns immediately with the context error
	if _, err := c.GetCompute(ctx, "1"); err == nil {
		t.Fatal("want error from cancelled context")
	}
}

func TestDo_BackoffSleepCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// First attempt returns a retryable error, then we cancel so the backoff
		// sleep before the retry fails.
		cancel()
		writeError(w, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "boom")
	}))
	defer srv.Close()
	c := newTestClient(srv.URL)
	if _, err := c.GetCompute(ctx, "1"); err == nil {
		t.Fatal("want error when backoff sleep is cancelled")
	}
}

func TestDo_NewRequestError(t *testing.T) {
	c := newTestClient("http://127.0.0.1:0")
	// A method string containing a space is rejected by http.NewRequestWithContext.
	if _, err := c.do(context.Background(), "BAD METHOD", "/x", nil, nil, requestOptions{}); err == nil {
		t.Fatal("want error building request with an invalid method")
	}
}

func TestNewClient_Defaults(t *testing.T) {
	// Empty base URL -> production default; rps<=0 -> API limit; nil httpClient.
	c := NewClient(Config{})
	if c.baseURL != defaultBaseURL {
		t.Errorf("baseURL = %q, want %q", c.baseURL, defaultBaseURL)
	}
	if c.userAgent != "terraform-provider-strettchcloud" {
		t.Errorf("userAgent = %q", c.userAgent)
	}
	if c.httpClient == nil {
		t.Error("httpClient should default")
	}
	if float64(c.limiter.Limit()) != apiRateLimit {
		t.Errorf("rps = %v, want %v", c.limiter.Limit(), apiRateLimit)
	}
}

func TestNewClient_RateCapAndTrim(t *testing.T) {
	c := NewClient(Config{BaseURL: "https://x/api/v1/", RequestsPerSecond: 99})
	if c.baseURL != "https://x/api/v1" {
		t.Errorf("trailing slash not trimmed: %q", c.baseURL)
	}
	if float64(c.limiter.Limit()) != apiRateLimit {
		t.Errorf("rps not capped to %v: %v", apiRateLimit, c.limiter.Limit())
	}
}

func TestUserAgentOrDefault(t *testing.T) {
	if got := (Config{UserAgent: "ua"}).userAgentOrDefault(); got != "ua" {
		t.Errorf("got %q", got)
	}
	if got := (Config{}).userAgentOrDefault(); got != "terraform-provider-strettchcloud" {
		t.Errorf("got %q", got)
	}
}

func TestDo_BodyMarshalError(t *testing.T) {
	c := newTestClient("http://127.0.0.1:0")
	// A channel cannot be JSON-encoded.
	_, err := c.do(context.Background(), http.MethodPost, "/x", make(chan int), nil, requestOptions{})
	if err == nil || !strings.Contains(err.Error(), "encoding request body") {
		t.Fatalf("want encode error, got %v", err)
	}
}

func TestDo_RetryExhaustionReturnsAPIError(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		writeError(w, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "boom")
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.GetCompute(context.Background(), "1")
	var apiErr *APIError
	if !asAPIError(err, &apiErr) || apiErr.Status != http.StatusInternalServerError {
		t.Fatalf("want 500 APIError, got %v", err)
	}
	if calls != 5 { // 1 initial + 4 retries
		t.Fatalf("calls = %d, want 5", calls)
	}
}

func TestDo_SuccessFalseEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":false,"code":"NOPE","message":"no","requestId":"r1"}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.GetCompute(context.Background(), "1")
	var apiErr *APIError
	if !asAPIError(err, &apiErr) || apiErr.Code != "NOPE" || apiErr.RequestID != "r1" {
		t.Fatalf("want NOPE APIError, got %v", err)
	}
}

func TestDo_EnvelopeDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{not json`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.GetCompute(context.Background(), "1")
	if err == nil || !strings.Contains(err.Error(), "decoding response envelope") {
		t.Fatalf("want envelope decode error, got %v", err)
	}
}

func TestDo_DataDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// data is a string but the caller decodes into a Compute struct.
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true,"data":"not-an-object"}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.GetCompute(context.Background(), "1")
	if err == nil || !strings.Contains(err.Error(), "decoding response data") {
		t.Fatalf("want data decode error, got %v", err)
	}
}

func TestDo_NetworkErrorRetries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // now connections fail
	c := newTestClient(url)
	_, err := c.GetCompute(context.Background(), "1")
	if err == nil {
		t.Fatal("want network error")
	}
}

func TestDo_429WaitsForReset(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set(headerRateReset, "1") // 1 second
			writeError(w, http.StatusTooManyRequests, "TOO_MANY_REQUESTS", "slow")
			return
		}
		writeEnvelope(w, http.StatusOK, Compute{ID: "1", State: StateRunning}, nil)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	start := time.Now()
	if _, err := c.GetCompute(context.Background(), "1"); err != nil {
		t.Fatalf("GetCompute: %v", err)
	}
	if time.Since(start) < 900*time.Millisecond {
		t.Errorf("should have waited ~1s for reset, waited %s", time.Since(start))
	}
}

func TestBackoff(t *testing.T) {
	c := &Client{retryWaitMin: time.Second, retryWaitMax: 10 * time.Second}
	if got := c.backoff(1); got != time.Second {
		t.Errorf("attempt1 = %v", got)
	}
	if got := c.backoff(3); got != 4*time.Second {
		t.Errorf("attempt3 = %v", got)
	}
	if got := c.backoff(99); got != 10*time.Second {
		t.Errorf("overflow should cap at max, got %v", got)
	}
	zero := &Client{retryWaitMin: 0, retryWaitMax: 5 * time.Second}
	if got := zero.backoff(1); got != 5*time.Second {
		t.Errorf("zero min should return max, got %v", got)
	}
}

func TestParseAPIError_Fallbacks(t *testing.T) {
	resp := &http.Response{StatusCode: http.StatusBadGateway}
	e := parseAPIError(resp, []byte(``))
	if e.Code != "BAD_GATEWAY" || e.Message != "Bad Gateway" {
		t.Errorf("fallback = %+v", e)
	}
	e2 := parseAPIError(&http.Response{StatusCode: http.StatusBadRequest}, []byte(`{"code":"X","message":"m","requestId":"r"}`))
	if e2.Code != "X" || e2.Message != "m" || e2.RequestID != "r" {
		t.Errorf("envelope = %+v", e2)
	}
}

func TestParseRateReset(t *testing.T) {
	cases := map[string]time.Duration{
		"":     0,
		"abc":  0,
		"0":    0,
		"5":    5 * time.Second,
		"-3":   0,
		"  7 ": 7 * time.Second,
	}
	for in, want := range cases {
		if got := parseRateReset(in); got != want {
			t.Errorf("parseRateReset(%q) = %v, want %v", in, got, want)
		}
	}
	// A unix-epoch-looking value in the past -> 0.
	if got := parseRateReset("1000000001"); got != 0 {
		t.Errorf("past epoch should be 0, got %v", got)
	}
}

func TestSleep(t *testing.T) {
	if err := sleep(context.Background(), 0); err != nil {
		t.Errorf("zero sleep: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleep(ctx, time.Minute); err == nil {
		t.Error("cancelled ctx should error")
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if firstNonEmpty("a", "b") != "a" {
		t.Error("want a")
	}
	if firstNonEmpty("", "b") != "b" {
		t.Error("want b")
	}
}

func TestMutatingComputeMethods(t *testing.T) {
	var lastMethod, lastPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastMethod, lastPath = r.Method, r.URL.Path
		writeEnvelope(w, http.StatusAccepted, Compute{ID: "1", State: StateStopped}, nil)
	}))
	defer srv.Close()
	c := newTestClient(srv.URL)
	ctx := context.Background()

	host := "h"
	tags := []string{"t"}
	if _, err := c.UpdateCompute(ctx, "1", ComputeUpdateRequest{HostName: &host, Tags: &tags}); err != nil {
		t.Fatalf("UpdateCompute: %v", err)
	}
	if lastMethod != http.MethodPatch || lastPath != "/computes/1" {
		t.Errorf("update hit %s %s", lastMethod, lastPath)
	}
	if _, err := c.UpsizeCompute(ctx, "1", "S-2C-4G"); err != nil {
		t.Fatalf("UpsizeCompute: %v", err)
	}
	if lastPath != "/computes/1/upsize" {
		t.Errorf("upsize path %s", lastPath)
	}
	for _, fn := range []func(context.Context, string) (*Compute, error){c.StartCompute, c.StopCompute, c.RebootCompute} {
		if _, err := fn(ctx, "1"); err != nil {
			t.Fatalf("action: %v", err)
		}
	}
}

func TestDeleteCompute_Non404Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "no")
	}))
	defer srv.Close()
	c := newTestClient(srv.URL)
	if err := c.DeleteCompute(context.Background(), "1"); err == nil {
		t.Fatal("403 should be returned as error")
	}
}

func TestCatalogErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "no")
	}))
	defer srv.Close()
	c := newTestClient(srv.URL)
	ctx := context.Background()
	if _, err := c.ListImages(ctx); err == nil {
		t.Error("ListImages should error")
	}
	if _, err := c.ListRegions(ctx); err == nil {
		t.Error("ListRegions should error")
	}
	if _, err := c.ListAllSpecifications(ctx); err == nil {
		t.Error("ListAllSpecifications should error")
	}
	if _, _, err := c.ListComputes(ctx, 1, 10); err == nil {
		t.Error("ListComputes should error")
	}
	if _, err := c.ListAllComputes(ctx); err == nil {
		t.Error("ListAllComputes should error")
	}
}

func TestCatalogSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/images"):
			writeEnvelope(w, http.StatusOK, []Image{{Image: "UBUNTU-24.04"}}, nil)
		case strings.HasSuffix(r.URL.Path, "/regions"):
			writeEnvelope(w, http.StatusOK, []Region{{Region: "KGL-1", ID: 1}}, nil)
		case strings.HasSuffix(r.URL.Path, "/specifications"):
			writeEnvelope(w, http.StatusOK, []Specification{{Specification: "S-1C-1G"}}, &PaginationMeta{Total: 1, Page: 1, Limit: 100})
		}
	}))
	defer srv.Close()
	c := newTestClient(srv.URL)
	ctx := context.Background()
	if imgs, err := c.ListImages(ctx); err != nil || len(imgs) != 1 {
		t.Errorf("images: %v %v", imgs, err)
	}
	if regs, err := c.ListRegions(ctx); err != nil || len(regs) != 1 {
		t.Errorf("regions: %v %v", regs, err)
	}
	if specs, err := c.ListAllSpecifications(ctx); err != nil || len(specs) != 1 {
		t.Errorf("specs: %v %v", specs, err)
	}
}

func TestListAllComputes_SinglePageNoMeta(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeEnvelope(w, http.StatusOK, makeComputes(3), nil) // no meta
	}))
	defer srv.Close()
	c := newTestClient(srv.URL)
	all, err := c.ListAllComputes(context.Background())
	if err != nil || len(all) != 3 {
		t.Fatalf("got %d (%v)", len(all), err)
	}
}

func TestWaitForState_DestroyedTarget404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "RESOURCE_NOT_FOUND", "gone")
	}))
	defer srv.Close()
	c := newTestClient(srv.URL)
	got, err := c.WaitForState(context.Background(), "1", "", StateDestroyed)
	if err != nil || got != nil {
		t.Fatalf("404 for destroyed target should be success: got=%v err=%v", got, err)
	}
}

func TestWaitForState_GetErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "no")
	}))
	defer srv.Close()
	c := newTestClient(srv.URL)
	if _, err := c.WaitForState(context.Background(), "1", "", StateRunning); err == nil {
		t.Fatal("want propagated error")
	}
	if _, err := c.WaitForRunning(context.Background(), "1"); err == nil {
		t.Fatal("WaitForRunning want propagated error")
	}
}

func TestWaitForState_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeEnvelope(w, http.StatusOK, Compute{ID: "1", State: StateStarting}, nil) // never settles
	}))
	defer srv.Close()
	c := newTestClient(srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.WaitForState(ctx, "1", "", StateRunning); err == nil {
		t.Fatal("want timeout error")
	}
	if _, err := c.WaitForRunning(ctx, "1"); err == nil {
		t.Fatal("WaitForRunning want timeout error")
	}
}

func TestIsNotFoundAndStable(t *testing.T) {
	if IsNotFound(context.DeadlineExceeded) {
		t.Error("non-APIError should not be NotFound")
	}
	if IsNotFound(&APIError{Status: 500}) {
		t.Error("500 is not NotFound")
	}
	if !IsStableState(StateStopped) || IsStableState(StateStarting) {
		t.Error("IsStableState wrong")
	}
}

func makeComputes(n int) []Compute {
	out := make([]Compute, n)
	for i := range out {
		out[i] = Compute{ID: fmt.Sprintf("c-%d", i), State: StateRunning}
	}
	return out
}

// asAPIError is a tiny test helper wrapping errors.As for readability.
func asAPIError(err error, target **APIError) bool {
	return errors.As(err, target)
}
