package integration

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWaitlist_JoinIsIdempotent(t *testing.T) {
	app := buildAPI(t)
	email := uniqueEmail("early-")
	for i := 0; i < 2; i++ {
		status, body := doJSON(t, app, "POST", "/v1/waitlist", "", map[string]any{"email": email, "source": "landing"})
		require.Equal(t, http.StatusOK, status)
		require.Equal(t, true, body["joined"])
	}
	status, _ := doJSON(t, app, "POST", "/v1/waitlist", "", map[string]any{"email": "not-an-email"})
	require.Equal(t, http.StatusBadRequest, status)
}
