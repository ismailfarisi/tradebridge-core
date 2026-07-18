import type { Metadata } from "next";
import LegalShell from "../LegalShell";

export const metadata: Metadata = {
  title: "Cookie Policy — TradeBridge",
};

export default function CookiesPage() {
  return (
    <LegalShell title="Cookie Policy" updated="[DATE]">
      <section>
        <p>
          This Cookie Policy explains how <strong>[Entity Name]</strong>
          uses cookies and similar technologies on the TradeBridge platform.
        </p>
      </section>

      <section>
        <h2>Essential cookies</h2>
        <p>
          These are required for the Service to function — for example,
          keeping you signed in, remembering your cookie choice, and
          protecting against fraud. Essential cookies are always active and
          cannot be disabled, as the Service will not work correctly
          without them.
        </p>
      </section>

      <section>
        <h2>Non-essential cookies</h2>
        <p>
          With your consent, we may use additional cookies to understand
          how the Service is used (analytics) and to measure the
          effectiveness of our marketing. These are only set if you accept
          them in the cookie banner.
        </p>
      </section>

      <section>
        <h2>Your choice</h2>
        <p>
          When you first visit TradeBridge, you can accept or reject
          non-essential cookies via the banner shown at the bottom of the
          page. Rejecting only disables non-essential cookies — essential
          cookies remain active so you can stay signed in and use the
          Service.
        </p>
      </section>

      <section>
        <h2>Changing your choice</h2>
        <p>
          To change your cookie choice, clear the{" "}
          <code>tb_cookie_consent</code> value from your browser&apos;s site
          data for this domain and reload the page — the consent banner
          will be shown again. A dedicated preferences control may be added
          here in a future update.
        </p>
      </section>

      <section>
        <h2>Contact</h2>
        <p>
          Questions about this Cookie Policy can be sent to{" "}
          <a href="mailto:[contact@example.com]">[contact@example.com]</a>.
        </p>
      </section>
    </LegalShell>
  );
}
