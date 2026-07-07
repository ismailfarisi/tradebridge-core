package cryptobox

import "testing"

func TestSealOpenRoundTrip(t *testing.T) {
	b, err := New("a-deployment-secret")
	if err != nil {
		t.Fatal(err)
	}
	ct, err := b.Seal("sk-super-secret-key")
	if err != nil {
		t.Fatal(err)
	}
	if string(ct) == "sk-super-secret-key" {
		t.Fatal("ciphertext must not equal plaintext")
	}
	pt, err := b.Open(ct)
	if err != nil {
		t.Fatal(err)
	}
	if pt != "sk-super-secret-key" {
		t.Fatalf("open = %q, want the original", pt)
	}
}

func TestOpenWithWrongKeyFails(t *testing.T) {
	a, _ := New("secret-a")
	b, _ := New("secret-b")
	ct, _ := a.Seal("hello")
	if _, err := b.Open(ct); err == nil {
		t.Fatal("open with the wrong key must fail")
	}
}

func TestTamperedCiphertextFails(t *testing.T) {
	b, _ := New("secret")
	ct, _ := b.Seal("hello")
	ct[len(ct)-1] ^= 0xff
	if _, err := b.Open(ct); err == nil {
		t.Fatal("tampered ciphertext must fail authentication")
	}
}

func TestEmptySecretRejected(t *testing.T) {
	if _, err := New(""); err == nil {
		t.Fatal("empty secret must be rejected")
	}
}
