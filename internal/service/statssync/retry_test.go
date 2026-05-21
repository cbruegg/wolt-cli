package statssync

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	woltgateway "github.com/mekedron/wolt-cli/internal/gateway/wolt"
)

type recordingSleeper struct {
	durations []time.Duration
	err       error
}

func (r *recordingSleeper) sleep(_ context.Context, d time.Duration) error {
	r.durations = append(r.durations, d)
	return r.err
}

func TestCallWithBackoffRetriesOn429AndHonorsRetryAfter(t *testing.T) {
	upstream429 := &woltgateway.UpstreamRequestError{
		Method:     http.MethodGet,
		URL:        "https://example/order/p1",
		StatusCode: 429,
		RetryAfter: 7 * time.Second,
	}
	calls := 0
	sleeper := &recordingSleeper{}
	var log bytes.Buffer
	result, err := callWithBackoff(
		context.Background(),
		func(context.Context) (map[string]any, error) {
			calls++
			if calls == 1 {
				return nil, upstream429
			}
			return map[string]any{"ok": true}, nil
		},
		sleeper.sleep,
		backoffPolicy{MaxAttempts: 3, BaseDelay: 2 * time.Second, MaxDelay: 60 * time.Second},
		nil,
		&log,
		"order detail p1",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result["ok"].(bool) {
		t.Fatalf("expected ok payload, got %+v", result)
	}
	if calls != 2 {
		t.Fatalf("expected 2 calls (1 fail + 1 retry), got %d", calls)
	}
	if len(sleeper.durations) != 1 || sleeper.durations[0] != 7*time.Second {
		t.Fatalf("expected single 7s sleep honoring Retry-After, got %v", sleeper.durations)
	}
	if !strings.Contains(log.String(), "order detail p1") || !strings.Contains(log.String(), "429") {
		t.Fatalf("expected progress log to mention label + status, got:\n%s", log.String())
	}
}

func TestCallWithBackoffExponentialWhenNoRetryAfter(t *testing.T) {
	upstream429 := &woltgateway.UpstreamRequestError{StatusCode: 429}
	sleeper := &recordingSleeper{}
	calls := 0
	_, err := callWithBackoff(
		context.Background(),
		func(context.Context) (map[string]any, error) {
			calls++
			return nil, upstream429
		},
		sleeper.sleep,
		backoffPolicy{MaxAttempts: 4, BaseDelay: 1 * time.Second, MaxDelay: 60 * time.Second},
		nil,
		nil,
		"order detail p1",
	)
	if err == nil {
		t.Fatal("expected error after exhausting attempts")
	}
	if !errors.Is(err, woltgateway.ErrUpstream) {
		t.Fatalf("expected wrapped upstream error, got %v", err)
	}
	if calls != 4 {
		t.Fatalf("expected 4 attempts, got %d", calls)
	}
	want := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}
	if len(sleeper.durations) != len(want) {
		t.Fatalf("expected %d sleeps between %d attempts, got %v", len(want), calls, sleeper.durations)
	}
	for i, d := range want {
		if sleeper.durations[i] != d {
			t.Fatalf("expected sleep[%d]=%s, got %s", i, d, sleeper.durations[i])
		}
	}
}

func TestCallWithBackoffCapsAtMaxDelay(t *testing.T) {
	upstream429 := &woltgateway.UpstreamRequestError{StatusCode: 429, RetryAfter: 5 * time.Minute}
	sleeper := &recordingSleeper{}
	_, err := callWithBackoff(
		context.Background(),
		func(context.Context) (map[string]any, error) { return nil, upstream429 },
		sleeper.sleep,
		backoffPolicy{MaxAttempts: 2, BaseDelay: 1 * time.Second, MaxDelay: 30 * time.Second},
		nil,
		nil,
		"order detail p1",
	)
	if err == nil {
		t.Fatal("expected exhausted error")
	}
	if len(sleeper.durations) != 1 || sleeper.durations[0] != 30*time.Second {
		t.Fatalf("expected single 30s sleep (capped), got %v", sleeper.durations)
	}
}

func TestCallWithBackoffDoesNotRetryOtherStatuses(t *testing.T) {
	upstream500 := &woltgateway.UpstreamRequestError{StatusCode: 500}
	sleeper := &recordingSleeper{}
	calls := 0
	_, err := callWithBackoff(
		context.Background(),
		func(context.Context) (map[string]any, error) {
			calls++
			return nil, upstream500
		},
		sleeper.sleep,
		backoffPolicy{MaxAttempts: 5, BaseDelay: 1 * time.Second, MaxDelay: 60 * time.Second},
		nil,
		nil,
		"order detail p1",
	)
	if err == nil {
		t.Fatal("expected error to propagate")
	}
	if calls != 1 {
		t.Fatalf("500 must not be retried; got %d calls", calls)
	}
	if len(sleeper.durations) != 0 {
		t.Fatalf("expected no sleeps for non-retriable status, got %v", sleeper.durations)
	}
}

func TestCallWithBackoffPropagatesCtxCancel(t *testing.T) {
	upstream429 := &woltgateway.UpstreamRequestError{StatusCode: 429, RetryAfter: time.Second}
	cancelErr := errors.New("ctx canceled")
	sleeper := &recordingSleeper{err: cancelErr}
	calls := 0
	_, err := callWithBackoff(
		context.Background(),
		func(context.Context) (map[string]any, error) {
			calls++
			return nil, upstream429
		},
		sleeper.sleep,
		backoffPolicy{MaxAttempts: 5, BaseDelay: 1 * time.Second, MaxDelay: 10 * time.Second},
		nil,
		nil,
		"order detail p1",
	)
	if !errors.Is(err, cancelErr) {
		t.Fatalf("expected cancel error to surface, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected single call before sleep aborted, got %d", calls)
	}
}

func TestAdjustablePacerBumpsAndCaps(t *testing.T) {
	// base=100ms, bump=50ms per 429, extra capped at 100ms (so total caps
	// at 200ms after two 429s).
	pacer := newAdjustablePacer(100*time.Millisecond, 50*time.Millisecond, 100*time.Millisecond, nil)
	if got := pacer.current(); got != 100*time.Millisecond {
		t.Fatalf("base current: want 100ms, got %s", got)
	}
	if got := pacer.noteRateLimited(); got != 150*time.Millisecond {
		t.Fatalf("after 1st 429: want 150ms, got %s", got)
	}
	if got := pacer.noteRateLimited(); got != 200*time.Millisecond {
		t.Fatalf("after 2nd 429: want 200ms, got %s", got)
	}
	if got := pacer.noteRateLimited(); got != 200*time.Millisecond {
		t.Fatalf("after 3rd 429 should stay capped at 200ms, got %s", got)
	}
	sleeper := &recordingSleeper{}
	if err := pacer.wait(context.Background(), sleeper.sleep); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if len(sleeper.durations) != 1 || sleeper.durations[0] != 200*time.Millisecond {
		t.Fatalf("wait should sleep capped 200ms, got %v", sleeper.durations)
	}
}

func TestCallWithBackoffBumpsPacerOnEvery429(t *testing.T) {
	pacer := newAdjustablePacer(100*time.Millisecond, 200*time.Millisecond, 10*time.Second, nil)
	sleeper := &recordingSleeper{}
	upstream429 := &woltgateway.UpstreamRequestError{StatusCode: 429}
	calls := 0
	_, err := callWithBackoff(
		context.Background(),
		func(context.Context) (map[string]any, error) {
			calls++
			if calls < 3 {
				return nil, upstream429
			}
			return map[string]any{"ok": true}, nil
		},
		sleeper.sleep,
		backoffPolicy{MaxAttempts: 5, BaseDelay: time.Second, MaxDelay: 10 * time.Second},
		pacer,
		nil,
		"order detail p1",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Two 429s before success → pacer bumped twice → base 100ms + 2*200ms = 500ms.
	if got := pacer.current(); got != 500*time.Millisecond {
		t.Fatalf("expected pacer current 500ms after two 429s, got %s", got)
	}
}

func TestIsRateLimitedRecognizesBoth429And503(t *testing.T) {
	for _, code := range []int{429, 503} {
		if !isRateLimited(&woltgateway.UpstreamRequestError{StatusCode: code}) {
			t.Fatalf("status %d should be rate-limited", code)
		}
	}
	for _, code := range []int{200, 400, 401, 404, 500} {
		if isRateLimited(&woltgateway.UpstreamRequestError{StatusCode: code}) {
			t.Fatalf("status %d should not be rate-limited", code)
		}
	}
	if isRateLimited(errors.New("plain error")) {
		t.Fatal("plain error must not be classified as rate-limited")
	}
}
