import type { Metadata } from "next";
import LegalShell from "../LegalShell";

export const metadata: Metadata = {
  title: "Terms of Service",
  description: "TradeBridge Terms of Service — the rules for using the platform.",
};

export default function TermsPage() {
  return (
    <LegalShell title="Terms of Service" updated="[DATE]">
      <section>
        <h2>1. Acceptance of terms</h2>
        <p>
          By creating an account or otherwise accessing or using the
          TradeBridge platform (the &quot;Service&quot;), operated by{" "}
          <strong>[Entity Name]</strong> (&quot;we&quot;, &quot;us&quot;), you
          agree to be bound by these Terms of Service (&quot;Terms&quot;). If
          you do not agree, do not use the Service.
        </p>
      </section>

      <section>
        <h2>2. Accounts</h2>
        <p>
          You must provide accurate registration information and keep your
          credentials secure. You are responsible for all activity under your
          account, including actions taken by team members you invite and by
          any AI agent acting on your behalf within the Service.
        </p>
      </section>

      <section>
        <h2>3. Acceptable use</h2>
        <p>
          You agree not to: misrepresent your identity or business; post
          fraudulent listings, quotes, or reviews; attempt to circumvent
          rate limits, verification, or safety controls; use the Service to
          transmit unlawful, infringing, or harmful content; or interfere
          with the operation of the Service.
        </p>
      </section>

      <section>
        <h2>4. Subscriptions and billing</h2>
        <p>
          Paid plans are billed in advance on a recurring basis via our
          payment processor. Fees are non-refundable except as required by
          law or as expressly stated at checkout. We may change plan pricing
          or features with prior notice; continued use after the effective
          date constitutes acceptance of the change. You may cancel at any
          time; cancellation takes effect at the end of the current billing
          period.
        </p>
      </section>

      <section>
        <h2>5. Disclaimers</h2>
        <p>
          The Service is provided &quot;as is&quot; and &quot;as
          available&quot; without warranties of any kind, express or
          implied. We do not guarantee the accuracy of listings, quotes, or
          counterparties, and we are not a party to transactions between
          buyers and sellers.
        </p>
      </section>

      <section>
        <h2>6. Limitation of liability</h2>
        <p>
          To the maximum extent permitted by law, [Entity Name] will not be
          liable for any indirect, incidental, special, consequential, or
          punitive damages, or any loss of profits or revenues, arising out
          of or related to your use of the Service.
        </p>
      </section>

      <section>
        <h2>7. Termination</h2>
        <p>
          We may suspend or terminate your access to the Service for
          breach of these Terms, suspected fraud or abuse, or as required
          by law. You may stop using the Service and delete your account at
          any time from your account settings.
        </p>
      </section>

      <section>
        <h2>8. Governing law</h2>
        <p>
          These Terms are governed by the laws of{" "}
          <strong>[Jurisdiction]</strong>, without regard to its conflict of
          laws principles.
        </p>
      </section>

      <section>
        <h2>Contact</h2>
        <p>
          Questions about these Terms can be sent to{" "}
          <a href="mailto:[contact@example.com]">[contact@example.com]</a>.
        </p>
      </section>
    </LegalShell>
  );
}
