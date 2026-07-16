package notify

import (
	"fmt"
	"html"
	"strings"
)

// renderEventEmail turns a notification Event into an HTML+text email pair.
func renderEventEmail(ev Event, baseURL string) (htmlBody, textBody string) {
	var ctaURL string
	if ev.CTAPath != "" {
		ctaURL = joinURL(baseURL, ev.CTAPath)
	}
	return renderEmail(ev.Title, ev.Body, ev.CTALabel, ctaURL, baseURL)
}

// RenderVerificationEmail builds the email sent to confirm a user's address.
func RenderVerificationEmail(baseURL, token string) (subject, htmlBody, textBody string) {
	subject = "Verify your TradeBridge email"
	verifyURL := joinURL(baseURL, "/verify?token="+token)
	body := "Welcome to TradeBridge. Confirm this is your email address to finish setting up your account."
	htmlBody, textBody = renderEmail(subject, body, "Verify email", verifyURL, baseURL)
	return subject, htmlBody, textBody
}

// renderEmail wraps a title/body/CTA in an inline-CSS, email-client-safe HTML
// shell (no <style> block, since many clients strip it), plus a matching
// plaintext alternative. All interpolated values are HTML-escaped.
func renderEmail(title, body, ctaLabel, ctaURL, baseURL string) (htmlBody, textBody string) {
	prefsURL := joinURL(baseURL, "/settings/notifications")

	safeTitle := html.EscapeString(title)
	safeBody := html.EscapeString(body)
	safeCTALabel := html.EscapeString(ctaLabel)
	safeCTAURL := html.EscapeString(ctaURL)
	safePrefsURL := html.EscapeString(prefsURL)

	var ctaHTML string
	if ctaLabel != "" && ctaURL != "" {
		ctaHTML = fmt.Sprintf(
			`<p style="margin:24px 0;"><a href="%s" style="background-color:#111827;color:#ffffff;display:inline-block;padding:12px 24px;border-radius:6px;text-decoration:none;font-family:sans-serif;font-size:14px;font-weight:600;">%s</a></p>`,
			safeCTAURL, safeCTALabel,
		)
	}

	htmlBody = fmt.Sprintf(`<div style="background-color:#f3f4f6;padding:32px 0;font-family:sans-serif;">
<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" style="max-width:560px;margin:0 auto;background-color:#ffffff;border-radius:8px;">
<tr><td style="padding:32px;">
<h1 style="margin:0 0 16px;font-size:20px;color:#111827;">%s</h1>
<p style="margin:0 0 16px;font-size:14px;line-height:1.6;color:#374151;">%s</p>
%s
</td></tr>
<tr><td style="padding:16px 32px;border-top:1px solid #e5e7eb;">
<p style="margin:0;font-size:12px;color:#9ca3af;">You can manage your <a href="%s" style="color:#6b7280;">notification preferences</a> at any time.</p>
</td></tr>
</table>
</div>`, safeTitle, safeBody, ctaHTML, safePrefsURL)

	var textParts []string
	textParts = append(textParts, title, "", body)
	if ctaLabel != "" && ctaURL != "" {
		textParts = append(textParts, "", fmt.Sprintf("%s: %s", ctaLabel, ctaURL))
	}
	textParts = append(textParts, "", fmt.Sprintf("Manage notification preferences: %s", prefsURL))
	textBody = strings.Join(textParts, "\n")

	return htmlBody, textBody
}

// joinURL joins a base URL and a path without doubling the slash seam.
func joinURL(base, path string) string {
	if base == "" {
		return path
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
}
