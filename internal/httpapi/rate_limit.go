package httpapi

import (
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type visitor struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type rateLimiter struct {
	mu          sync.Mutex
	visitors    map[string]*visitor
	limit       rate.Limit
	burst       int
	ttl         time.Duration
	lastCleanup time.Time
	now         func() time.Time
}

type rateLimitKey func(*http.Request) string

func newRateLimiter(eventsPerMinute, burst int) *rateLimiter {
	return &rateLimiter{
		visitors: make(map[string]*visitor),
		limit:    rate.Every(time.Minute / time.Duration(eventsPerMinute)),
		burst:    burst,
		ttl:      15 * time.Minute,
		now:      time.Now,
	}
}

func (limiter *rateLimiter) middleware(key rateLimitKey) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			if !limiter.allow(key(request)) {
				response.Header().Set("Retry-After", "60")
				writeAPIError(response, request, http.StatusTooManyRequests, "RATE_LIMIT_EXCEEDED", "Too many requests; try again later")
				return
			}
			next.ServeHTTP(response, request)
		})
	}
}

func (limiter *rateLimiter) allow(key string) bool {
	now := limiter.now()
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	if limiter.lastCleanup.IsZero() || now.Sub(limiter.lastCleanup) >= limiter.ttl {
		for existingKey, existing := range limiter.visitors {
			if now.Sub(existing.lastSeen) >= limiter.ttl {
				delete(limiter.visitors, existingKey)
			}
		}
		limiter.lastCleanup = now
	}
	existing, found := limiter.visitors[key]
	if !found {
		existing = &visitor{limiter: rate.NewLimiter(limiter.limit, limiter.burst)}
		limiter.visitors[key] = existing
	}
	existing.lastSeen = now
	return existing.limiter.AllowN(now, 1)
}

func clientIPKey(request *http.Request) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err == nil {
		return host
	}
	return request.RemoteAddr
}

func identityKey(request *http.Request) string {
	if principal, ok := principalFromContext(request.Context()); ok {
		return principal.IdentityID.String()
	}
	return clientIPKey(request)
}
