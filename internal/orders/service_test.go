package orders

import "testing"

func TestCanTransition(t *testing.T) {
	ok := [][2]string{
		{"created", "confirmed"}, {"created", "cancelled"}, {"created", "disputed"},
		{"confirmed", "fulfilled"}, {"confirmed", "cancelled"}, {"confirmed", "disputed"},
		{"fulfilled", "completed"}, {"fulfilled", "disputed"},
		{"disputed", "completed"}, {"disputed", "cancelled"},
	}
	for _, p := range ok {
		if !CanTransition(p[0], p[1]) {
			t.Errorf("expected %s->%s allowed", p[0], p[1])
		}
	}
	bad := [][2]string{
		{"created", "fulfilled"}, {"created", "completed"}, {"fulfilled", "cancelled"},
		{"completed", "confirmed"}, {"cancelled", "confirmed"}, {"fulfilled", "confirmed"},
	}
	for _, p := range bad {
		if CanTransition(p[0], p[1]) {
			t.Errorf("expected %s->%s disallowed", p[0], p[1])
		}
	}
}

func TestRoleFor(t *testing.T) {
	cases := map[string]string{
		"confirmed": "seller", "fulfilled": "seller",
		"completed": "buyer", "cancelled": "either", "disputed": "either",
	}
	for to, want := range cases {
		if got := RoleFor(to); got != want {
			t.Errorf("RoleFor(%s)=%s want %s", to, got, want)
		}
	}
}

func TestPartyTargetsExcludesDisputed(t *testing.T) {
	if PartyTargets["disputed"] {
		t.Error("disputed must not be a party-driven target (goes through dispute endpoint)")
	}
	for _, s := range []string{"confirmed", "fulfilled", "completed", "cancelled"} {
		if !PartyTargets[s] {
			t.Errorf("%s should be a party target", s)
		}
	}
}
