"use client";
import { useRef, useState } from "react";
import { uploadAttachment, type AttachmentMeta } from "@/lib/api";
import { ACCEPT, MAX_PER_MESSAGE, validateFile } from "@/lib/attachments";

export function Composer({
  draft,
  setDraft,
  onSend,
  disabled,
  token,
  placeholder = "Describe what you need…",
}: {
  draft: string;
  setDraft: (v: string) => void;
  onSend: (attachments: AttachmentMeta[]) => void | Promise<void>;
  disabled: boolean;
  token: string;
  placeholder?: string;
}) {
  const [files, setFiles] = useState<File[]>([]);
  const [uploading, setUploading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const fileRef = useRef<HTMLInputElement>(null);
  const uploadedRef = useRef(new Map<File, AttachmentMeta>());

  const pick = (list: FileList | null) => {
    if (!list) return;
    setError(null);
    const next = [...files];
    for (const f of Array.from(list)) {
      const problem = validateFile(f.name, f.type, f.size);
      if (problem) {
        setError(problem);
        continue;
      }
      if (next.length >= MAX_PER_MESSAGE) {
        setError(`Max ${MAX_PER_MESSAGE} attachments per message`);
        break;
      }
      next.push(f);
    }
    setFiles(next);
    if (fileRef.current) fileRef.current.value = "";
  };

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (disabled || uploading || (!draft.trim() && files.length === 0)) return;
    setError(null);
    setUploading(true);
    try {
      const metas: AttachmentMeta[] = [];
      for (const f of files) {
        const cached = uploadedRef.current.get(f);
        if (cached) {
          metas.push(cached);
          continue;
        }
        const meta = await uploadAttachment(token, f);
        uploadedRef.current.set(f, meta);
        metas.push(meta);
      }
      setFiles([]);
      uploadedRef.current.clear();
      await onSend(metas);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Upload failed — retry");
    } finally {
      setUploading(false);
    }
  };

  return (
    <form onSubmit={submit} style={{ borderTop: "1px solid var(--border)", padding: "0.9rem 1.5rem" }}>
      <div className="col" style={{ gap: "0.5rem", maxWidth: 760, margin: "0 auto" }}>
        {files.length > 0 && (
          <div className="row" style={{ gap: "0.4rem", flexWrap: "wrap" }}>
            {files.map((f, i) => (
              <span key={`${f.name}-${i}`} className="badge badge-info">
                {f.type === "application/pdf" ? "📄" : "🖼"} {f.name}
                <button
                  type="button"
                  className="btn btn-ghost btn-sm"
                  aria-label={`remove ${f.name}`}
                  onClick={() => {
                    uploadedRef.current.delete(f);
                    setFiles(files.filter((_, j) => j !== i));
                  }}
                >
                  ✕
                </button>
              </span>
            ))}
          </div>
        )}
        {error && <div className="err">{error}</div>}
        <div className="row" style={{ gap: "0.6rem" }}>
          <input
            ref={fileRef}
            type="file"
            accept={ACCEPT}
            multiple
            style={{ display: "none" }}
            onChange={(e) => pick(e.target.files)}
          />
          <button
            type="button"
            className="btn btn-ghost"
            aria-label="attach files"
            title="Attach images or PDFs"
            onClick={() => fileRef.current?.click()}
            disabled={disabled || uploading}
          >
            📎
          </button>
          <input
            className="input"
            style={{ flex: 1 }}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            placeholder={placeholder}
            autoFocus
          />
          <button className="btn btn-primary" disabled={disabled || uploading || (!draft.trim() && files.length === 0)}>
            {uploading ? "Uploading…" : "Send"}
          </button>
        </div>
      </div>
    </form>
  );
}
