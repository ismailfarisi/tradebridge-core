"use client";

import { useReducer, useState } from "react";
import { useRouter } from "next/navigation";
import { useToken } from "@/lib/auth";
import { rfqChatStream, type AttachmentMeta } from "@/lib/api";
import { chatReducer, initialChatState } from "@/lib/chat";
import { Composer } from "@/components/Composer";
import { AttachmentView } from "@/components/AttachmentView";
import { AssistImprove } from "@/components/AssistImprove";
import { Icon } from "@/components/Icon";

const SAMPLE_RFQS = [
  "Need 500 corrugated shipping cartons (30×30×30cm), delivered to Dubai within 3 weeks.",
  "Looking for 1,000 stainless steel M8 bolts, grade 8.8 — quote per unit and lead time.",
  "Sourcing 200 reams of A4 80gsm copy paper for a Riyadh office, recurring monthly.",
];

export default function NewChat() {
  const token = useToken();
  const router = useRouter();
  const [state, dispatch] = useReducer(chatReducer, initialChatState);
  const [draft, setDraft] = useState("");

  const send = async (attachments: AttachmentMeta[]) => {
    const content = draft.trim();
    if ((!content && attachments.length === 0) || !token || state.pending) return;
    const ids = attachments.map((a) => a.id);
    const next = [
      ...state.messages,
      {
        role: "user" as const,
        content,
        ...(ids.length ? { attachment_ids: ids, attachments } : {}),
      },
    ];
    dispatch({ type: "send", content, attachmentIds: ids, attachments });
    setDraft("");
    try {
      const res = await rfqChatStream(token, next, (text) => dispatch({ type: "delta", text }));
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
          <div style={{ textAlign: "center", marginTop: "12vh" }}>
            <h1 style={{ fontSize: "1.5rem" }}>What do you need to source?</h1>
            <p className="muted" style={{ marginTop: "0.5rem" }}>
              Describe it in plain language — I&apos;ll match sellers and collect quotes. Include quantity,
              specs, destination, and any deadline.
            </p>
            <div className="col" style={{ gap: "0.5rem", maxWidth: 560, margin: "1.5rem auto 0" }}>
              <p className="muted small" style={{ margin: 0 }}>Try an example:</p>
              {SAMPLE_RFQS.map((s) => (
                <button
                  key={s}
                  type="button"
                  className="sample-prompt"
                  onClick={() => setDraft(s)}
                >
                  <Icon name="sparkles" size={16} />
                  <span>{s}</span>
                </button>
              ))}
            </div>
          </div>
        ) : (
          <div className="col" style={{ gap: "0.75rem" }}>
            {state.messages.map((m, i) => (
              <Bubble key={i} role={m.role} content={m.content} attachments={m.attachments} token={token} />
            ))}
            {state.pending && !state.streaming && <p className="muted small">…</p>}
            {state.error && <p className="err">{state.error}</p>}
          </div>
        )}
      </div>
      <Composer
        draft={draft}
        setDraft={setDraft}
        onSend={send}
        disabled={state.pending}
        token={token ?? ""}
        assist={token && <AssistImprove token={token} draft={draft} onSuggestion={setDraft} disabled={state.pending} />}
      />
    </div>
  );
}

function Bubble({
  role,
  content,
  attachments,
  token,
}: {
  role: string;
  content: string;
  attachments?: AttachmentMeta[];
  token: string | null;
}) {
  const mine = role === "user";
  return (
    <div style={{ display: "flex", justifyContent: mine ? "flex-end" : "flex-start" }}>
      <div
        className="card"
        style={{
          maxWidth: "80%",
          background: mine ? "var(--primary)" : "var(--surface)",
          color: mine ? "#fff" : "var(--text)",
          borderColor: mine ? "var(--primary)" : undefined,
          padding: "0.6rem 0.85rem",
        }}
      >
        {content}
        {attachments?.map((a) => (
          <div key={a.id} style={{ marginTop: "0.35rem" }}>
            <AttachmentView meta={a} token={token ?? ""} />
          </div>
        ))}
      </div>
    </div>
  );
}
