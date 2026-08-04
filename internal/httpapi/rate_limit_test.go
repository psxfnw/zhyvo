package httpapi

import (
	"testing"
	"time"
)

func TestRateLimiterBurstAndRefill(t *testing.T) {
	current := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
	limiter := newRateLimiter(60, 2)
	limiter.now = func() time.Time { return current }

	if !limiter.allow("client") || !limiter.allow("client") {
		t.Fatal("initial burst was rejected")
	}
	if limiter.allow("client") {
		t.Fatal("request above burst was allowed")
	}
	current = current.Add(time.Second)
	if !limiter.allow("client") {
		t.Fatal("token was not refilled")
	}
}
