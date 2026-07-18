"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { PageHeader } from "@/components/AppShell";
import { useMe } from "@/lib/auth";
import { clearToken } from "@/lib/api";
import { canDelete, deleteAccount, exportData } from "@/lib/account";

export default function AccountPage() {
  const { token, me } = useMe();
  const router = useRouter();

  const [exporting, setExporting] = useState(false);
  const [exportErr, setExportErr] = useState<string | null>(null);

  const [confirmText, setConfirmText] = useState("");
  const [password, setPassword] = useState("");
  const [deleting, setDeleting] = useState(false);
  const [deleteErr, setDeleteErr] = useState<string | null>(null);

  const doExport = async () => {
    if (!token) return;
    setExporting(true);
    setExportErr(null);
    try {
      const bundle = await exportData(token);
      const blob = new Blob([JSON.stringify(bundle, null, 2)], { type: "application/json" });
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = "tradebridge-export.json";
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);
    } catch (e) {
      setExportErr(e instanceof Error ? e.message : "export failed");
    } finally {
      setExporting(false);
    }
  };

  const doDelete = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!token) return;
    setDeleting(true);
    setDeleteErr(null);
    try {
      await deleteAccount(token, password);
      clearToken();
      router.push("/");
    } catch (e) {
      setDeleteErr(e instanceof Error ? e.message : "delete failed");
      setDeleting(false);
    }
  };

  return (
    <>
      <PageHeader title="Account" subtitle="Export your data, or permanently delete your account." />

      <div className="card" style={{ maxWidth: 560, marginBottom: "1.25rem" }}>
        <h2 style={{ fontSize: "1.05rem", marginBottom: "0.75rem" }}>Export my data</h2>
        <p className="muted" style={{ marginBottom: "0.75rem" }}>
          Download a JSON bundle of everything associated with your company: profile, catalog, RFQs,
          quotes, orders, invoices, reviews, and disputes.
        </p>
        {exportErr && <p className="err">{exportErr}</p>}
        <button className="btn btn-primary" onClick={doExport} disabled={exporting || !token}>
          {exporting ? "Exporting…" : "Export my data"}
        </button>
      </div>

      {me && canDelete(me.role) && (
        <div className="card" style={{ maxWidth: 560, borderColor: "var(--danger, #c0392b)" }}>
          <h2 style={{ fontSize: "1.05rem", marginBottom: "0.75rem" }}>Danger zone</h2>
          <p className="muted" style={{ marginBottom: "0.75rem" }}>
            Deleting your account soft-deletes your company, anonymizes user emails and passwords, scrubs
            your supplier profile, and revokes all agent credentials. This cannot be undone from the app.
          </p>
          <form className="col" onSubmit={doDelete}>
            <div className="field">
              <label>Type DELETE to confirm</label>
              <input
                className="input"
                value={confirmText}
                onChange={(e) => setConfirmText(e.target.value)}
                placeholder="DELETE"
              />
            </div>
            <div className="field">
              <label>Password</label>
              <input
                className="input"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder="Your password"
              />
            </div>
            {deleteErr && <p className="err">{deleteErr}</p>}
            <div>
              <button
                className="btn btn-primary"
                type="submit"
                disabled={deleting || confirmText !== "DELETE" || !password}
              >
                {deleting ? "Deleting…" : "Delete my account"}
              </button>
            </div>
          </form>
        </div>
      )}
    </>
  );
}
