package httpretry

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestDo_SucceedsFirstTry(t *testing.T) {
	calls := 0
	result, err := Do(context.Background(), Options{}, func(ctx context.Context, attempt int) (string, error) {
		calls++
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "ok" {
		t.Errorf("result = %q, want %q", result, "ok")
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

func TestDo_RetriesRetryableErrorsThenSucceeds(t *testing.T) {
	calls := 0
	opts := Options{MaxRetries: 3, BackoffBase: time.Millisecond, BackoffCap: 5 * time.Millisecond}
	result, err := Do(context.Background(), opts, func(ctx context.Context, attempt int) (int, error) {
		calls++
		if attempt < 2 {
			return 0, &StatusError{StatusCode: http.StatusTooManyRequests, URL: "http://example.test"}
		}
		return 42, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != 42 {
		t.Errorf("result = %d, want 42", result)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3 (2 failures + 1 success)", calls)
	}
}

func TestDo_StopsImmediatelyOnNonRetryableStatus(t *testing.T) {
	calls := 0
	opts := Options{MaxRetries: 5, BackoffBase: time.Millisecond}
	_, err := Do(context.Background(), opts, func(ctx context.Context, attempt int) (int, error) {
		calls++
		return 0, &StatusError{StatusCode: http.StatusNotFound, URL: "http://example.test"}
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (404 is not retryable, should not retry)", calls)
	}
}

func TestDo_ExhaustsMaxRetriesAndReturnsLastError(t *testing.T) {
	calls := 0
	opts := Options{MaxRetries: 2, BackoffBase: time.Millisecond, BackoffCap: 2 * time.Millisecond}
	_, err := Do(context.Background(), opts, func(ctx context.Context, attempt int) (int, error) {
		calls++
		return 0, &StatusError{StatusCode: http.StatusInternalServerError, URL: "http://example.test"}
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if calls != 3 { // 1 initial + 2 retries
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestDo_RespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	_, err := Do(ctx, Options{MaxRetries: 5}, func(ctx context.Context, attempt int) (int, error) {
		calls++
		return 0, errors.New("should not be called")
	})
	if err == nil {
		t.Fatal("expected context error, got nil")
	}
	if calls != 0 {
		t.Errorf("calls = %d, want 0 (should not attempt with an already-cancelled context)", calls)
	}
}

func TestIsRetryableStatus(t *testing.T) {
	cases := map[int]bool{
		http.StatusOK:                  false,
		http.StatusNotFound:            false,
		http.StatusBadRequest:          false,
		http.StatusTooManyRequests:     true,
		http.StatusForbidden:           true,
		http.StatusInternalServerError: true,
		http.StatusBadGateway:          true,
		http.StatusServiceUnavailable:  true,
	}
	for code, want := range cases {
		if got := IsRetryableStatus(code); got != want {
			t.Errorf("IsRetryableStatus(%d) = %v, want %v", code, got, want)
		}
	}
}

func TestIsRetryable_NonStatusErrorTreatedAsRetryable(t *testing.T) {
	if !IsRetryable(errors.New("connection reset")) {
		t.Error("plain (non-StatusError) errors should be treated as retryable — network blips etc.")
	}
}
