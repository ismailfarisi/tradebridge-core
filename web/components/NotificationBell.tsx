"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import {
  listNotifications,
  markNotificationRead,
  markAllNotificationsRead,
  type Notification,
} from "@/lib/notifications";

const POLL_MS = 30_000;

export default function NotificationBell({ token }: { token: string }) {
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const [items, setItems] = useState<Notification[]>([]);
  const [unread, setUnread] = useState(0);
  const boxRef = useRef<HTMLDivElement | null>(null);

  const refresh = useCallback(async () => {
    try {
      const out = await listNotifications(token, { limit: 15 });
      setItems(out.notifications);
      setUnread(out.unread);
    } catch {
      /* best-effort; leave prior state */
    }
  }, [token]);

  useEffect(() => {
    refresh();
    const id = setInterval(refresh, POLL_MS);
    return () => clearInterval(id);
  }, [refresh]);

  // Close on outside click.
  useEffect(() => {
    if (!open) return;
    const onClick = (e: MouseEvent) => {
      if (boxRef.current && !boxRef.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", onClick);
    return () => document.removeEventListener("mousedown", onClick);
  }, [open]);

  async function onItem(n: Notification) {
    if (!n.read) {
      try { await markNotificationRead(token, n.id); } catch { /* ignore */ }
      setItems((xs) => xs.map((x) => (x.id === n.id ? { ...x, read: true } : x)));
      setUnread((u) => Math.max(0, u - 1));
    }
    const path = typeof n.data?.cta_path === "string" ? (n.data.cta_path as string) : null;
    if (path) { setOpen(false); router.push(path); }
  }

  async function onMarkAll() {
    try { await markAllNotificationsRead(token); } catch { /* ignore */ }
    setItems((xs) => xs.map((x) => ({ ...x, read: true })));
    setUnread(0);
  }

  return (
    <div ref={boxRef} style={{ position: "relative" }}>
      <button
        className="btn btn-sm btn-ghost"
        aria-label={`Notifications${unread ? ` (${unread} unread)` : ""}`}
        onClick={() => { setOpen((o) => !o); if (!open) refresh(); }}
        style={{ position: "relative" }}
      >
        <span aria-hidden>🔔</span>
        {unread > 0 && (
          <span
            style={{
              position: "absolute", top: -4, right: -4, minWidth: 16, height: 16,
              padding: "0 4px", borderRadius: 8, background: "var(--danger)", color: "#fff",
              fontSize: 10, lineHeight: "16px", textAlign: "center", fontWeight: 700,
            }}
          >
            {unread > 9 ? "9+" : unread}
          </span>
        )}
      </button>

      {open && (
        <div
          className="card"
          style={{
            position: "absolute", right: 0, top: "calc(100% + 8px)", width: 340, maxHeight: 420,
            overflowY: "auto", zIndex: 50, padding: 0,
          }}
        >
          <div className="row" style={{ justifyContent: "space-between", padding: "0.6rem 0.8rem", borderBottom: "1px solid var(--border)" }}>
            <strong>Notifications</strong>
            {unread > 0 && <button className="btn btn-sm btn-ghost" onClick={onMarkAll}>Mark all read</button>}
          </div>
          {items.length === 0 ? (
            <div className="muted small" style={{ padding: "1rem 0.8rem" }}>No notifications yet.</div>
          ) : (
            items.map((n) => (
              <button
                key={n.id}
                onClick={() => onItem(n)}
                style={{
                  display: "block", width: "100%", textAlign: "left", cursor: "pointer",
                  background: n.read ? "transparent" : "var(--surface-2)",
                  border: "none", borderBottom: "1px solid var(--border)", padding: "0.6rem 0.8rem", color: "inherit",
                }}
              >
                <div className="row" style={{ justifyContent: "space-between", gap: "0.5rem" }}>
                  <strong style={{ fontSize: 13 }}>{n.title}</strong>
                  {!n.read && <span className="dot" aria-hidden style={{ background: "var(--primary)" }} />}
                </div>
                <div className="muted small" style={{ marginTop: 2 }}>{n.body}</div>
              </button>
            ))
          )}
        </div>
      )}
    </div>
  );
}
