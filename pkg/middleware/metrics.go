package middleware

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
)

// metricKey identifies one label combination: HTTP method, the registered
// ROUTE TEMPLATE (e.g. "/v1/rfqs/:id" — never the raw request path), and the
// response status class ("2xx".."5xx"). Because the route template comes
// from Fiber's route registration (a finite, fixed set of paths), the map
// below has bounded cardinality and never leaks ids/tenant data.
type metricKey struct {
	method string
	route  string
	status string
}

// Metrics is a dependency-free, in-memory Prometheus-style collector for the
// request golden signals: request count and request duration (sum+count,
// i.e. a "summary" good enough for average latency) per (method, route,
// status class). No external dependency (no prometheus/client_golang) — the
// exposition text is hand-written with fmt/strconv.
type Metrics struct {
	mu       sync.Mutex
	counts   map[metricKey]uint64
	durSum   map[metricKey]float64
	durCount map[metricKey]uint64
}

// NewMetrics builds an empty collector.
func NewMetrics() *Metrics {
	return &Metrics{
		counts:   make(map[metricKey]uint64),
		durSum:   make(map[metricKey]float64),
		durCount: make(map[metricKey]uint64),
	}
}

// statusClass reduces an exact HTTP status code to its Prometheus-style
// class label ("2xx", "4xx", ...), never the raw code — keeps cardinality
// bounded and avoids encoding anything request-specific into labels.
func statusClass(code int) string {
	class := code / 100
	if class < 1 || class > 5 {
		return "xxx"
	}
	return strconv.Itoa(class) + "xx"
}

// Middleware records one observation per request: method, the registered
// route TEMPLATE, the response status class, and the elapsed handler
// duration. c.Route().Path is only populated once Fiber has matched the
// route, which happens inside c.Next() — so it must be read AFTER c.Next()
// returns, not before. It never reads c.Path()/c.OriginalURL(), so no id or
// tenant data ever reaches a label.
//
// Status derivation: Fiber's DefaultErrorHandler (no custom ErrorHandler is
// configured for this app) only rewrites the response status AFTER every
// middleware has unwound — so a handler/guard that returns a *fiber.Error
// (e.g. middleware.JWT, TenantGuard, RequireActiveCompany, RequireVerified,
// PlatformAdminGuard all return 401/403 this way) still reads back as 200 at
// this point in the stack. To avoid mislabeling every auth rejection as
// "2xx", the status is derived from the returned error the same way
// DefaultErrorHandler would: a *fiber.Error's Code, or 500 for any other
// non-nil error, taking priority over c.Response().StatusCode().
func (m *Metrics) Middleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()

		err := c.Next()

		elapsed := time.Since(start).Seconds()
		route := c.Route().Path
		method := c.Method()

		code := c.Response().StatusCode()
		if err != nil {
			code = fiber.StatusInternalServerError
			var fe *fiber.Error
			if errors.As(err, &fe) {
				code = fe.Code
			}
		}
		status := statusClass(code)
		k := metricKey{method: method, route: route, status: status}

		m.mu.Lock()
		m.counts[k]++
		m.durSum[k] += elapsed
		m.durCount[k]++
		m.mu.Unlock()

		return err
	}
}

// Handler serves the current snapshot as Prometheus text exposition format.
func (m *Metrics) Handler() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(fiber.HeaderContentType, "text/plain; version=0.0.4; charset=utf-8")
		return c.SendString(m.render())
	}
}

// render snapshots the collector under lock and writes the Prometheus text
// exposition. Keys are sorted for stable, human-diffable output.
func (m *Metrics) render() string {
	m.mu.Lock()
	keys := make([]metricKey, 0, len(m.counts))
	counts := make(map[metricKey]uint64, len(m.counts))
	durSum := make(map[metricKey]float64, len(m.durSum))
	durCount := make(map[metricKey]uint64, len(m.durCount))
	for k, v := range m.counts {
		keys = append(keys, k)
		counts[k] = v
		durSum[k] = m.durSum[k]
		durCount[k] = m.durCount[k]
	}
	m.mu.Unlock()

	sort.Slice(keys, func(i, j int) bool {
		if keys[i].method != keys[j].method {
			return keys[i].method < keys[j].method
		}
		if keys[i].route != keys[j].route {
			return keys[i].route < keys[j].route
		}
		return keys[i].status < keys[j].status
	})

	var b strings.Builder
	b.WriteString("# HELP http_requests_total Total HTTP requests\n")
	b.WriteString("# TYPE http_requests_total counter\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "http_requests_total{%s} %d\n", labels(k), counts[k])
	}

	b.WriteString("# HELP http_request_duration_seconds_sum Sum of HTTP request durations in seconds\n")
	b.WriteString("# TYPE http_request_duration_seconds_sum summary\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "http_request_duration_seconds_sum{%s} %s\n", labels(k), strconv.FormatFloat(durSum[k], 'f', -1, 64))
	}

	b.WriteString("# HELP http_request_duration_seconds_count Count of observed HTTP request durations\n")
	b.WriteString("# TYPE http_request_duration_seconds_count summary\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "http_request_duration_seconds_count{%s} %d\n", labels(k), durCount[k])
	}

	return b.String()
}

// labels renders a metricKey as Prometheus label text. Route templates and
// methods come from Fiber's fixed route table, not request-supplied data, so
// no escaping beyond quoting is needed.
func labels(k metricKey) string {
	return fmt.Sprintf(`method=%q,route=%q,status=%q`, k.method, k.route, k.status)
}
