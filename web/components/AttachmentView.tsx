"use client";
import { useEffect, useState } from "react";
import { fetchAttachmentBlob, type AttachmentMeta } from "@/lib/api";

// Renders one attachment: images inline, PDFs as a chip that opens in a new
// tab. Fetches via authenticated fetch → object URL (plain <img src> can't
// send the bearer token).
export function AttachmentView({ meta, token }: { meta: AttachmentMeta; token: string }) {
  const [url, setUrl] = useState<string | null>(null);
  const [err, setErr] = useState(false);

  useEffect(() => {
    let obj: string | null = null;
    let alive = true;
    fetchAttachmentBlob(token, meta.id)
      .then((blob) => {
        const o = URL.createObjectURL(blob);
        if (!alive) {
          URL.revokeObjectURL(o);
          return;
        }
        obj = o;
        setUrl(o);
      })
      .catch(() => alive && setErr(true));
    return () => {
      alive = false;
      if (obj) URL.revokeObjectURL(obj);
    };
  }, [meta.id, token]);

  if (err) return <span className="muted">⚠ {meta.filename} unavailable</span>;
  if (!url) return <span className="muted">… {meta.filename}</span>;

  if (meta.mime.startsWith("image/")) {
    return (
      <a href={url} target="_blank" rel="noreferrer">
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img
          src={url}
          alt={meta.filename}
          style={{ maxHeight: 160, maxWidth: "100%", borderRadius: 8, border: "1px solid var(--border)" }}
        />
      </a>
    );
  }
  return (
    <a className="badge badge-info" href={url} target="_blank" rel="noreferrer">
      📄 {meta.filename}
    </a>
  );
}
