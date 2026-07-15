"use client";
import { useCallback, useEffect, useState } from "react";
import { postQuoteMessage, quoteMessages, type AttachmentMeta, type QuoteMessage } from "@/lib/api";
import { AttachmentView } from "@/components/AttachmentView";
import { Composer } from "@/components/Composer";

// The buyer↔seller-agent thread under one quote: where the agent's clarifying
// questions appear and the buyer answers (optionally with images/PDFs).
export function QuoteThread({ quoteId, token }: { quoteId: string; token: string }) {
  const [messages, setMessages] = useState<QuoteMessage[]>([]);
  const [draft, setDraft] = useState("");
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    try {
      const res = await quoteMessages(token, quoteId);
      setMessages(res.messages);
    } catch {
      /* transient poll errors are fine */
    }
  }, [quoteId, token]);

  useEffect(() => {
    // Initial fetch + 4s poll for new clarification messages (same cadence
    // the page already uses for quotes).
    // eslint-disable-next-line react-hooks/set-state-in-effect
    refresh();
    const t = setInterval(refresh, 4000);
    return () => clearInterval(t);
  }, [refresh]);

  const send = async (attachments: AttachmentMeta[]) => {
    const body = draft.trim();
    if (!body && attachments.length === 0) return;
    setSending(true);
    setError(null);
    try {
      await postQuoteMessage(token, quoteId, body, attachments.map((a) => a.id));
      setDraft("");
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Couldn't send — retry");
    } finally {
      setSending(false);
    }
  };

  return (
    <div className="col" style={{ gap: "0.5rem", marginTop: "0.6rem" }}>
      {messages.map((m) => (
        <div
          key={m.id}
          className="card"
          style={{
            padding: "0.5rem 0.75rem",
            alignSelf: m.author === "buyer" ? "flex-end" : "flex-start",
            maxWidth: "85%",
          }}
        >
          <div className="muted" style={{ fontSize: "0.75rem" }}>
            {m.author === "buyer" ? "You" : "Seller agent"}
          </div>
          {m.body && <div>{m.body}</div>}
          {m.attachments?.map((a) => (
            <div key={a.id} style={{ marginTop: "0.35rem" }}>
              <AttachmentView meta={a} token={token} />
            </div>
          ))}
        </div>
      ))}
      {messages.length === 0 && <div className="muted">No messages yet.</div>}
      {error && <div className="err">{error}</div>}
      <Composer
        draft={draft}
        setDraft={setDraft}
        onSend={send}
        disabled={sending}
        token={token}
        placeholder="Reply to the seller agent…"
      />
    </div>
  );
}
