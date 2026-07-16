package notify

import (
	"strings"
	"testing"
)

func TestRenderVerificationEmail(t *testing.T) {
	subject, html, text := RenderVerificationEmail("https://app.example.com", "tok123")
	if !strings.Contains(subject, "Verify") {
		t.Errorf("subject = %q, want it to mention Verify", subject)
	}
	if !strings.Contains(html, "https://app.example.com/verify?token=tok123") {
		t.Errorf("html missing verify link: %s", html)
	}
	if !strings.Contains(text, "tok123") {
		t.Errorf("text missing token: %s", text)
	}
	// Preferences footer is mandatory on transactional mail.
	if !strings.Contains(html, "/settings/notifications") {
		t.Errorf("html missing preferences footer")
	}
}

func TestRenderEmail_EscapesHTML(t *testing.T) {
	html, _ := renderEmail("Title", "<script>alert(1)</script>", "", "", "https://x.com")
	if strings.Contains(html, "<script>alert(1)</script>") {
		t.Errorf("body HTML was not escaped: %s", html)
	}
}

func TestJoinURL(t *testing.T) {
	if got := joinURL("https://x.com/", "/a"); got != "https://x.com/a" {
		t.Errorf("joinURL = %q, want https://x.com/a", got)
	}
}
