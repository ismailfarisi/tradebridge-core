package rfq

import "testing"

func TestCanTransitionToExpired(t *testing.T) {
	cases := []struct {
		from, to string
		want     bool
	}{
		{StatusOpen, StatusExpired, true},
		{StatusQuoted, StatusExpired, true},
		{StatusDraft, StatusExpired, false},
		{StatusClosed, StatusExpired, false},
		{StatusExpired, StatusClosed, false},
		{StatusExpired, StatusOpen, false},
	}
	for _, c := range cases {
		if got := CanTransition(c.from, c.to); got != c.want {
			t.Errorf("CanTransition(%q,%q)=%v want %v", c.from, c.to, got, c.want)
		}
	}
}
