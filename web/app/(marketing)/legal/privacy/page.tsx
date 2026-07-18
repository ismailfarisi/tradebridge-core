import type { Metadata } from "next";
import Link from "next/link";
import LegalShell from "../LegalShell";

export const metadata: Metadata = {
  title: "Privacy Policy",
  description: "How TradeBridge collects, uses, and protects your data.",
};

export default function PrivacyPage() {
  return (
    <LegalShell title="Privacy Policy" updated="[DATE]">
      <section>
        <p>
          This Privacy Policy explains how <strong>[Entity Name]</strong>
          (&quot;we&quot;, &quot;us&quot;) collects, uses, and shares
          information when you use the TradeBridge platform (the
          &quot;Service&quot;).
        </p>
      </section>

      <section>
        <h2>Data we collect</h2>
        <ul>
          <li>
            Account data: name, email, company details, and role provided at
            signup.
          </li>
          <li>
            Transaction data: RFQs, quotes, messages, orders, invoices, and
            reviews you create or receive.
          </li>
          <li>
            Usage data: pages visited, features used, and device/browser
            information, collected via essential and (with consent)
            analytics cookies.
          </li>
          <li>
            Billing data: processed by our payment processor; we store plan
            and subscription status, not full card numbers.
          </li>
        </ul>
      </section>

      <section>
        <h2>Purposes</h2>
        <p>
          We use this data to operate and secure the Service, facilitate
          transactions between buyers and sellers, provide customer support,
          send transactional and (with consent) marketing communications,
          and improve the Service, including AI-assisted quoting features.
        </p>
      </section>

      <section>
        <h2>Legal bases</h2>
        <p>
          Where applicable (e.g. under GDPR), we process personal data based
          on: performance of a contract with you, our legitimate interests
          in operating and improving the Service, your consent (e.g. for
          non-essential cookies or marketing), and compliance with legal
          obligations.
        </p>
      </section>

      <section>
        <h2>Retention</h2>
        <p>
          We retain account and transaction data for as long as your
          account is active and as needed to comply with legal, tax, and
          accounting requirements. When you delete your account, we delete
          or anonymize personal data within a reasonable period, except
          where retention is required by law.
        </p>
      </section>

      <section>
        <h2>Your rights</h2>
        <p>
          Depending on your jurisdiction, you may have the right to access,
          export, correct, or delete your personal data. You can request a
          data export or delete your account directly from{" "}
          <Link href="/account">your account settings</Link>. For other
          requests, contact us using the details below.
        </p>
      </section>

      <section>
        <h2>Subprocessors</h2>
        <p>
          We use the following subprocessors to provide the Service:
        </p>
        <ul>
          <li>
            <strong>Stripe</strong> — payment processing and subscription
            billing.
          </li>
          <li>
            <strong>Resend</strong> — transactional email delivery.
          </li>
          <li>
            <strong>Anthropic</strong> — AI models powering agent quoting and
            buyer assist features.
          </li>
          <li>
            <strong>[Hosting provider]</strong> — application hosting and
            infrastructure.
          </li>
        </ul>
      </section>

      <section>
        <h2>Contact</h2>
        <p>
          Questions about this Privacy Policy or your data can be sent to{" "}
          <a href="mailto:[contact@example.com]">[contact@example.com]</a>.
        </p>
      </section>
    </LegalShell>
  );
}
