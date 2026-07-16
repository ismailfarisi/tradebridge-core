"use client";

import { useState } from "react";
import { joinWaitlist } from "@/lib/notifications";

export default function WaitlistForm() {
  const [email, setEmail] = useState("");
  const [state, setState] = useState<"idle" | "busy" | "done" | "error">("idle");

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (!email.includes("@")) { setState("error"); return; }
    setState("busy");
    try { await joinWaitlist(email, "landing"); setState("done"); }
    catch { setState("error"); }
  }

  if (state === "done") {
    return <p className="ok small">You&rsquo;re on the list — we&rsquo;ll be in touch.</p>;
  }

  return (
    <form onSubmit={submit} className="row" style={{ gap: "0.5rem", flexWrap: "wrap" }}>
      <input
        className="input"
        type="email"
        placeholder="you@company.com"
        value={email}
        onChange={(e) => { setEmail(e.target.value); if (state === "error") setState("idle"); }}
        aria-label="Email address"
        style={{ minWidth: 220 }}
      />
      <button className="btn btn-primary" type="submit" disabled={state === "busy"}>
        {state === "busy" ? "Joining…" : "Join the waitlist"}
      </button>
      {state === "error" && <span className="err small">Enter a valid email.</span>}
    </form>
  );
}
