"use client";

import { useReducer, useState } from "react";
import { useRouter } from "next/navigation";
import { useToken } from "@/lib/auth";
import { rfqChatStream, type AttachmentMeta } from "@/lib/api";
import { chatReducer, initialChatState } from "@/lib/chat";
import { Composer } from "@/components/Composer";
import { AttachmentView } from "@/components/AttachmentView";
import { AssistImprove } from "@/components/AssistImprove";

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
          <div style={{ textAlign: "center", marginTop: "18vh" }}>
            <h1 style={{ fontSize: "1.5rem" }}>What do you need to source?</h1>
            <p className="muted" style={{ marginTop: "0.5rem" }}>
              Describe it in plain language — I&apos;ll match sellers and collect quotes.
            </p>
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
          background: mine ? "var(--surface-2)" : "var(--surface)",
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
