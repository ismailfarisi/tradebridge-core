package trust

import (
	"context"
	"errors"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

type fakeResolver struct{ records map[string][]string }

func (f *fakeResolver) LookupTXT(_ context.Context, name string) ([]string, error) {
	if r, ok := f.records[name]; ok {
		return r, nil
	}
	return nil, errors.New("no such host")
}

func newSvc(t *testing.T, res TXTResolver) *Service {
	q, pool := testQueriesOrSkip(t)
	return NewService(q, pool, res, zerolog.Nop())
}

func TestClaimReturnsChallenge(t *testing.T) {
	svc := newSvc(t, &fakeResolver{})
	q, _ := testQueriesOrSkip(t)
	cid := makeCompany(t, q, "claim-co")

	ch, err := svc.Claim(context.Background(), cid, "  HTTPS://Acme-Claim.Example/path ")
	require.NoError(t, err)
	require.Equal(t, "acme-claim.example", ch.Domain, "normalized: lowercased, scheme/path stripped")
	require.Equal(t, "_tradebridge-verify.acme-claim.example", ch.RecordName)
	require.Equal(t, "TXT", ch.RecordType)
	require.Contains(t, ch.RecordValue, "tradebridge-verify=")

	got, err := q.GetCompanyVerification(context.Background(), cid)
	require.NoError(t, err)
	require.Equal(t, "pending", got.VerificationStatus)
}

func TestClaimRejectsBadDomain(t *testing.T) {
	svc := newSvc(t, &fakeResolver{})
	q, _ := testQueriesOrSkip(t)
	cid := makeCompany(t, q, "claim-bad-co")
	for _, bad := range []string{"", "nodot", "com", "has space.com", "a..b.com"} {
		_, err := svc.Claim(context.Background(), cid, bad)
		require.ErrorIs(t, err, ErrInvalidDomain, "domain %q must be rejected", bad)
	}
}

func TestCheckMissThenMatch(t *testing.T) {
	res := &fakeResolver{records: map[string][]string{}}
	svc := newSvc(t, res)
	q, pool := testQueriesOrSkip(t)
	// Persistent test DB: release any stale claim from a prior invocation
	// before SetCompanyVerified below re-claims this hardcoded domain.
	releaseDomain(t, pool, "check-co.example")
	t.Cleanup(func() { releaseDomain(t, pool, "check-co.example") })
	cid := makeCompany(t, q, "check-co")

	ch, err := svc.Claim(context.Background(), cid, "check-co.example")
	require.NoError(t, err)

	// Miss: no TXT record yet → stays pending, no error.
	r, err := svc.Check(context.Background(), cid, "check-co.example")
	require.NoError(t, err)
	require.False(t, r.Verified)
	require.Equal(t, "pending", r.Status)

	// Publish the record, then match.
	res.records[ch.RecordName] = []string{ch.RecordValue}
	r, err = svc.Check(context.Background(), cid, "check-co.example")
	require.NoError(t, err)
	require.True(t, r.Verified)
	require.Equal(t, "verified", r.Status)

	got, err := q.GetCompanyVerification(context.Background(), cid)
	require.NoError(t, err)
	require.Equal(t, "verified", got.VerificationStatus)
	require.Equal(t, "check-co.example", *got.VerifiedDomain)
}

func TestCheckDomainTaken(t *testing.T) {
	res := &fakeResolver{records: map[string][]string{}}
	svc := newSvc(t, res)
	q, pool := testQueriesOrSkip(t)
	a := makeCompany(t, q, "taken-a")
	b := makeCompany(t, q, "taken-b")
	const domain = "taken-shared.example"
	releaseDomain(t, pool, domain)
	t.Cleanup(func() { releaseDomain(t, pool, domain) })

	chA, err := svc.Claim(context.Background(), a, domain)
	require.NoError(t, err)
	chB, err := svc.Claim(context.Background(), b, domain)
	require.NoError(t, err)
	// Both companies' tokens are published; A wins the unique index, B collides.
	res.records[chA.RecordName] = []string{chA.RecordValue, chB.RecordValue}

	rA, err := svc.Check(context.Background(), a, domain)
	require.NoError(t, err)
	require.True(t, rA.Verified)

	_, err = svc.Check(context.Background(), b, domain)
	require.ErrorIs(t, err, ErrDomainTaken, "second company verifying same domain → 409 domain_taken, not 500")
}

func TestSetStatusRevokeResetsChallenge(t *testing.T) {
	res := &fakeResolver{records: map[string][]string{}}
	svc := newSvc(t, res)
	q, pool := testQueriesOrSkip(t)
	cid := makeCompany(t, q, "revoke-reset-co")
	const domain = "revoke-reset.example"
	releaseDomain(t, pool, domain)
	t.Cleanup(func() { releaseDomain(t, pool, domain) })

	ch, err := svc.Claim(context.Background(), cid, domain)
	require.NoError(t, err)
	res.records[ch.RecordName] = []string{ch.RecordValue}
	r, err := svc.Check(context.Background(), cid, domain)
	require.NoError(t, err)
	require.True(t, r.Verified)

	// Admin revokes. The domain is freed and the challenge demoted, so a
	// subsequent Check must NOT report verified from the stale row.
	require.NoError(t, svc.SetStatus(context.Background(), cid, "rejected", "abuse"))
	res.records = map[string][]string{} // domain reassigned away; TXT no longer present
	r, err = svc.Check(context.Background(), cid, domain)
	require.NoError(t, err)
	require.False(t, r.Verified, "revoked company cannot re-report verified from a stale challenge row")
	require.Equal(t, "pending", r.Status)
}

func TestCheckWithoutClaim(t *testing.T) {
	svc := newSvc(t, &fakeResolver{})
	q, _ := testQueriesOrSkip(t)
	cid := makeCompany(t, q, "noclaim-co")
	_, err := svc.Check(context.Background(), cid, "noclaim.example")
	require.ErrorIs(t, err, ErrNoClaim)
}

func TestSetStatusRevokeClearsDomain(t *testing.T) {
	res := &fakeResolver{records: map[string][]string{}}
	svc := newSvc(t, res)
	q, _ := testQueriesOrSkip(t)
	cid := makeCompany(t, q, "revoke-co")
	ch, _ := svc.Claim(context.Background(), cid, "revoke.example")
	res.records[ch.RecordName] = []string{ch.RecordValue}
	_, err := svc.Check(context.Background(), cid, "revoke.example")
	require.NoError(t, err)

	require.NoError(t, svc.SetStatus(context.Background(), cid, "rejected", "abuse"))
	got, err := q.GetCompanyVerification(context.Background(), cid)
	require.NoError(t, err)
	require.Equal(t, "rejected", got.VerificationStatus)
	require.Nil(t, got.VerifiedDomain, "revoke frees the domain")
}
