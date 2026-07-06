package gateway

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/internal/protocol"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

func TestParseSeq(t *testing.T) {
	cases := map[string]int64{"": 0, "abc": 0, "-4": 0, "0": 0, "42": 42}
	for in, want := range cases {
		if got := parseSeq(in); got != want {
			t.Errorf("parseSeq(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestWriteEventFrame(t *testing.T) {
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	writeEvent(w, protocol.Event{Seq: 7, ID: "E1", Type: "rfq.matched", Payload: []byte(`{"rfq_id":"R1"}`)})
	require.NoError(t, w.Flush())
	require.Equal(t, "id: 7\nevent: rfq.matched\ndata: {\"rfq_id\":\"R1\"}\n\n", buf.String())
}

// TestReplayCursorAndWatermark verifies that replaying since a Last-Event-ID
// emits only later events and returns the correct dedup watermark.
func TestReplayCursorAndWatermark(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping DB integration test")
	}
	pool, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	q := sqlc.New(pool)
	ctx := context.Background()

	company, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "ReplayCo", Type: "seller"})
	require.NoError(t, err)

	var seqs []int64
	for i := 0; i < 3; i++ {
		ev, err := q.CreateAgentEvent(ctx, sqlc.CreateAgentEventParams{
			CompanyID: company.ID, Type: "rfq.matched", Payload: []byte(`{"n":` + strconv.Itoa(i) + `}`),
		})
		require.NoError(t, err)
		seqs = append(seqs, ev.Seq)
	}

	h := &Handler{q: q, logger: zerolog.Nop()}

	// Fresh connect (no Last-Event-ID) → all unacked events, watermark = highest.
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	watermark, err := h.replay(ctx, w, company.ID, 0)
	require.NoError(t, err)
	require.Equal(t, seqs[2], watermark, "watermark is the highest replayed seq")
	require.Equal(t, 3, strings.Count(buf.String(), "event: rfq.matched"))

	// Replay since the first event → only the last two, first one excluded.
	buf.Reset()
	w = bufio.NewWriter(&buf)
	watermark, err = h.replay(ctx, w, company.ID, seqs[0])
	require.NoError(t, err)
	require.Equal(t, seqs[2], watermark)
	require.Equal(t, 2, strings.Count(buf.String(), "event: rfq.matched"))
	require.NotContains(t, buf.String(), "id: "+strconv.FormatInt(seqs[0], 10)+"\n")
}
