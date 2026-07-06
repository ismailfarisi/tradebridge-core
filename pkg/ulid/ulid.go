// Package ulid generates lexicographically sortable, time-ordered IDs.
// The database mints ULIDs by default via gen_ulid(); this package is for
// application-side generation (tests, event ordering, idempotency keys).
package ulid

import (
	"crypto/rand"

	"github.com/oklog/ulid/v2"
)

// New returns a new ULID string (26 chars, Crockford base32, uppercase).
func New() string {
	return ulid.MustNew(ulid.Now(), rand.Reader).String()
}
