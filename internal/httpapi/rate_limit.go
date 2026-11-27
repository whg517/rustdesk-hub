package httpapi

import (
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v5"
	"golang.org/x/time/rate"
)

const maxRateBuckets = 4096

type rateBucket struct {
	lastSeen time.Time
	limiter  *rate.Limiter
}

type keyedRateLimit struct {
	mu        sync.Mutex
	buckets   map[string]rateBucket
	nextSweep time.Time
	overflow  *rate.Limiter
}

func newKeyedRateLimit() *keyedRateLimit {
	return &keyedRateLimit{
		buckets:  make(map[string]rateBucket),
		overflow: rate.NewLimiter(rate.Every(2*time.Second), 8),
	}
}

func (limit *keyedRateLimit) allow(key string) bool {
	limit.mu.Lock()
	defer limit.mu.Unlock()
	now := time.Now()
	if now.After(limit.nextSweep) {
		for key, bucket := range limit.buckets {
			if now.Sub(bucket.lastSeen) > 5*time.Minute {
				delete(limit.buckets, key)
			}
		}
		limit.nextSweep = now.Add(time.Minute)
	}

	bucket, exists := limit.buckets[key]
	if !exists {
		if len(limit.buckets) >= maxRateBuckets {
			return limit.overflow.AllowN(now, 1)
		}
		bucket.limiter = rate.NewLimiter(rate.Every(2*time.Second), 8)
	}
	bucket.lastSeen = now
	limit.buckets[key] = bucket
	return bucket.limiter.AllowN(now, 1)
}

func loginSource(r *http.Request, behindProxy bool) string {
	if behindProxy {
		// The adjacent trusted proxy appends or overwrites the client address.
		forwarded := r.Header.Values("X-Forwarded-For")
		if len(forwarded) > 0 {
			last := forwarded[len(forwarded)-1]
			address := strings.TrimSpace(last[strings.LastIndexByte(last, ',')+1:])
			if ip, err := netip.ParseAddr(address); err == nil {
				return ip.Unmap().String()
			}
		}
	}
	address, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return "unknown"
	}
	return address.Addr().Unmap().String()
}

func rateLimited(c *echo.Context) error {
	c.Response().Header().Set("Retry-After", "2")
	return apiError{http.StatusTooManyRequests, "Request.RateLimited", "请求过于频繁，请稍后重试"}
}
