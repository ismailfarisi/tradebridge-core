"use client";

import { useReducer, useState } from "react";
import { useRouter } from "next/navigation";
import { useToken } from "@/lib/auth";
import { rfqChat } from "@/lib/api";
import { chatReducer, initialChatState } from "@/lib/chat";

export default function NewChat() {
  const token = useToken();
  const router = useRouter();
  const [state, dispatch] = useReducer(chatReducer, initialChatState);
  const [draft, setDraft] = useState("");

  const send = async (e: React.FormEvent) => {
    e.preventDefault();
    const content = draft.trim();
    if (!content || !token || state.pending) return;
    const next = [...state.messages, { role: "user" as const, content }];
    dispatch({ type: "send", content });
    setDraft("");
    try {
      const res = await rfqChat(token, next);
      if (res.done && res.rfq) {
        router.push(`/chat/${res.rfq.id}`);
        return;
      }
      dispatch({ type: "reply", assistant: res.assistant });
    } catch (err) {
      dispatch({ type: "error", error: err instanceof Error ? err.message : "Couldn't reach Core — retry" });
    }
  };

  const empty = state.messages.length === 0;

  return (
    <div style={{ flex: 1, display: "flex", flexDirection: "column", minWidth: 0 }}>
      <div style={{ flex: 1, overflowY: "auto", padding: "1.5rem", maxWidth: 760, width: "100%", margin: "0 auto" }}>
        {empty ? (
          <div style={{ textAlign: "center", marginTop: "18vh" }}>
            <h1 style={{ fontSize: "1.5rem" }}>What do you need to source?</h1>
            <p className="muted" style={{ marginTop: "0.5rem" }}>
              Describe it in plain language — I&apos;ll match sellers and collect quotes.
            </p>
          </div>
        ) : (
          <div className="col" style={{ gap: "0.75rem" }}>
            {state.messages.map((m, i) => (
              <Bubble key={i} role={m.role} content={m.content} />
            ))}
            {state.pending && <p className="muted small">…</p>}
            {state.error && <p className="err">{state.error}</p>}
          </div>
        )}
      </div>
      <Composer draft={draft} setDraft={setDraft} onSubmit={send} disabled={state.pending} />
    </div>
  );
}

function Bubble({ role, content }: { role: string; content: string }) {
  const mine = role === "user";
  return (
    <div style={{ display: "flex", justifyContent: mine ? "flex-end" : "flex-start" }}>
      <div
        className="card"
        style={{
          maxWidth: "80%",
          background: mine ? "var(--surface-2)" : "var(--surface)",
          padding: "0.6rem 0.85rem",
        }}
      >
        {content}
      </div>
    </div>
  );
}

export function Composer({
  draft,
  setDraft,
  onSubmit,
  disabled,
}: {
  draft: string;
  setDraft: (v: string) => void;
  onSubmit: (e: React.FormEvent) => void;
  disabled: boolean;
}) {
  return (
    <form
      onSubmit={onSubmit}
      style={{ borderTop: "1px solid var(--border)", padding: "0.9rem 1.5rem" }}
    >
      <div className="row" style={{ gap: "0.6rem", maxWidth: 760, margin: "0 auto" }}>
        <input
          className="input"
          style={{ flex: 1 }}
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          placeholder="Describe what you need…"
          autoFocus
        />
        <button className="btn btn-primary" disabled={disabled || !draft.trim()}>
          Send
        </button>
      </div>
    </form>
  );
}
