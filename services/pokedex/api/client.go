package api

import (
	"errors"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	ht "github.com/ogen-go/ogen/http"
)

// Package api is the generated ogen client plus the transport
// wrapper - retry, timeout, breaker - that ogen deliberately omits.
// Consumers get the whole set by importing this package rather than
// only the generated code.
//
//	c, err := api.NewClient(url, api.WithClient(api.NewHTTPClient(api.HTTPOptions{})))

// ClientVersion tracks the spec's info.version and is sent on every
// request so a server can see which callers are still on old
// versions.
const ClientVersion = "0.8.0"

// ClientVersionHeader has no X- prefix per RFC 6648.
const ClientVersionHeader = "Client-Version"

// ClientNameHeader identifies WHICH caller is making the request, so
// two services built from the same spec are separable in logs.
const ClientNameHeader = "Client-Name"

// RetryPolicy decides whether to repeat a failed request. len(Backoffs)
// is the retry count.
type RetryPolicy interface {
	Backoffs() []time.Duration
	Retry(req *http.Request, resp *http.Response, err error) bool
}

// SingleRetry retries once after a second. It is the default so a
// struggling dependency does not see many callers multiply its traffic.
type SingleRetry struct{}

func (SingleRetry) Backoffs() []time.Duration { return []time.Duration{time.Second} }
func (SingleRetry) Retry(req *http.Request, resp *http.Response, err error) bool {
	return retryable(req, resp, err)
}

// ExponentialRetry backs off with jitter across five attempts.
type ExponentialRetry struct{}

func (ExponentialRetry) Backoffs() []time.Duration {
	out := make([]time.Duration, 5)
	next := 100 * time.Millisecond
	for i := range out {
		// Jitter so a caller fleet does not retry in lockstep.
		out[i] = next + time.Duration((rand.Float64()*2-1)*0.25*float64(next))
		next *= 2
	}
	return out
}
func (ExponentialRetry) Retry(req *http.Request, resp *http.Response, err error) bool {
	return retryable(req, resp, err)
}

// NoRetry is for a caller that would rather fail fast.
type NoRetry struct{}

func (NoRetry) Backoffs() []time.Duration                       { return nil }
func (NoRetry) Retry(*http.Request, *http.Response, error) bool { return false }

// retryable repeats only idempotent methods on failures that say the
// request did not take effect. POST may already have applied.
func retryable(req *http.Request, resp *http.Response, err error) bool {
	switch req.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodPut, http.MethodDelete:
	default:
		return false
	}
	if err != nil {
		return true
	}
	return resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
}

// ErrCircuitOpen is returned instead of calling a failing dependency,
// so callers fail immediately rather than queueing behind a timeout.
var ErrCircuitOpen = errors.New("circuit open")

// Breaker trips after Threshold consecutive failures and lets one
// probe through every Cooldown.
type Breaker struct {
	Threshold int
	Cooldown  time.Duration

	mu       sync.Mutex
	failures int
	openedAt time.Time
}

func (b *Breaker) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.failures < b.Threshold {
		return true
	}
	if time.Since(b.openedAt) > b.Cooldown {
		b.failures = 0 // half-open: one probe decides whether to close
		return true
	}
	return false
}

func (b *Breaker) record(failed bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !failed {
		b.failures = 0
		return
	}
	b.failures++
	if b.failures == b.Threshold {
		b.openedAt = time.Now()
	}
}

// HTTPOptions configure the client. The zero value is the default set.
type HTTPOptions struct {
	// Timeout bounds a single attempt. Zero means 5s.
	Timeout time.Duration
	// Policy defaults to SingleRetry.
	Policy RetryPolicy
	// Breaker is optional; nil disables circuit breaking.
	Breaker *Breaker

	// MaxRetryAfter caps a server's Retry-After. Zero means 30s.
	MaxRetryAfter time.Duration

	// Name is the CALLER's service name, sent as Client-Name. Empty
	// sends nothing rather than guessing from the user agent.
	Name string
}

// HTTPClient satisfies ogen's ht.Client.
type HTTPClient struct {
	http          *http.Client
	policy        RetryPolicy
	breaker       *Breaker
	maxRetryAfter time.Duration
	name          string
}

var _ ht.Client = (*HTTPClient)(nil)

// NewHTTPClient builds a client with the defaults filled in.
func NewHTTPClient(o HTTPOptions) *HTTPClient {
	if o.Timeout == 0 {
		o.Timeout = 5 * time.Second
	}
	if o.Policy == nil {
		o.Policy = SingleRetry{}
	}
	if o.MaxRetryAfter == 0 {
		o.MaxRetryAfter = 30 * time.Second
	}
	return &HTTPClient{
		http:          &http.Client{Timeout: o.Timeout},
		policy:        o.Policy,
		breaker:       o.Breaker,
		maxRetryAfter: o.MaxRetryAfter,
		name:          o.Name,
	}
}

// retryAfter reads the server's Retry-After (RFC 9110: seconds or
// HTTP-date), or reports it as absent if it exceeds max.
func retryAfter(resp *http.Response, max time.Duration) (time.Duration, bool) {
	if resp == nil {
		return 0, false
	}
	v := resp.Header.Get("Retry-After")
	if v == "" {
		return 0, false
	}

	var d time.Duration
	if secs, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
		if secs < 0 {
			return 0, false
		}
		d = time.Duration(secs) * time.Second
	} else if t, err := http.ParseTime(v); err == nil {
		d = time.Until(t)
		if d < 0 {
			d = 0
		}
	} else {
		return 0, false // unparseable; fall back to the policy's backoff
	}

	if d > max {
		return 0, false // too long to wait inside a request
	}
	return d, true
}

func (c *HTTPClient) Do(req *http.Request) (*http.Response, error) {
	if c.breaker != nil && !c.breaker.allow() {
		return nil, ErrCircuitOpen
	}
	req.Header.Set(ClientVersionHeader, ClientVersion)
	if c.name != "" {
		req.Header.Set(ClientNameHeader, c.name)
	}

	backoffs := c.policy.Backoffs()
	var (
		resp *http.Response
		err  error
	)
	for attempt := 0; ; attempt++ {
		resp, err = c.http.Do(req)
		if attempt >= len(backoffs) || !c.policy.Retry(req, resp, err) {
			break
		}

		// Retry-After wins over the policy: the server knows when
		// capacity returns.
		wait := backoffs[attempt]
		if d, ok := retryAfter(resp, c.maxRetryAfter); ok {
			wait = d
		}

		// Close the body or the connection returns to the pool unusable.
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(wait)
	}
	if c.breaker != nil {
		c.breaker.record(err != nil || (resp != nil && resp.StatusCode >= http.StatusInternalServerError))
	}
	return resp, err
}
