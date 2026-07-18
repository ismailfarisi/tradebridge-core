package middleware

import (
	"io"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/require"
)

// TestMetricsMiddlewareRecordsRouteTemplateAndServesExposition builds a fiber
// app with the metrics middleware + a parameterized route + the /metrics
// handler, hits the route twice with different ids, then scrapes /metrics
// and checks the Prometheus text exposition: labels use the route TEMPLATE
// (never the raw id-bearing path), and the exposition is well-formed.
func TestMetricsMiddlewareRecordsRouteTemplateAndServesExposition(t *testing.T) {
	m := NewMetrics()
	app := fiber.New()
	app.Use(m.Middleware())
	app.Get("/x/:id", func(c *fiber.Ctx) error { return c.SendString("ok") })
	app.Get("/metrics", m.Handler())

	// Hit the templated route twice, with different ids, to prove the label
	// is the route template and not the raw path (no id leakage).
	for _, id := range []string{"123", "456"} {
		req := httptest.NewRequest(fiber.MethodGet, "/x/"+id, nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusOK, resp.StatusCode)
	}

	// First scrape: the middleware records a request's stats AFTER c.Next()
	// returns, i.e. after the /metrics handler has already built its response
	// body — so a scrape never reflects the request that produced it. Pick
	// this behavior ("/metrics doesn't count itself in its own response")
	// and assert it consistently below.
	resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/metrics", nil))
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	text := string(body)

	require.Contains(t, text, "# HELP http_requests_total")
	require.Contains(t, text, "# TYPE http_requests_total counter")
	require.Contains(t, text, `http_requests_total{method="GET",route="/x/:id",status="2xx"} 2`)
	require.Contains(t, text, "# HELP http_request_duration_seconds_sum")
	require.Contains(t, text, "# TYPE http_request_duration_seconds_sum summary")
	require.Contains(t, text, `http_request_duration_seconds_count{method="GET",route="/x/:id",status="2xx"} 2`)

	// No raw ids anywhere in the exposition — labels are route templates only.
	require.NotContains(t, text, "123")
	require.NotContains(t, text, "456")
	// This first scrape predates any recorded /metrics request.
	require.NotContains(t, text, `route="/metrics"`)

	// Second scrape: now reflects the FIRST /metrics request recorded above.
	resp2, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/metrics", nil))
	require.NoError(t, err)
	body2, err := io.ReadAll(resp2.Body)
	require.NoError(t, err)
	text2 := string(body2)
	require.Contains(t, text2, `http_requests_total{method="GET",route="/metrics",status="2xx"} 1`)

	// Well-formed exposition: every non-comment, non-blank line matches
	// `metric_name{label="value",...} number`.
	lineRe := regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*\{[^}]*\} [0-9]+(\.[0-9]+)?$`)
	for _, line := range strings.Split(strings.TrimRight(text2, "\n"), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		require.Regexp(t, lineRe, line, "malformed exposition line: %q", line)
	}
}
