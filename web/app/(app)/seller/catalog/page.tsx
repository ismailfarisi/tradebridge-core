"use client";

import { useEffect, useState } from "react";
import { PageHeader } from "@/components/AppShell";
import { EmptyState } from "@/components/EmptyState";
import { useToken } from "@/lib/auth";
import { createCatalog, deleteCatalog, listCatalog, type CatalogItem } from "@/lib/api";

export default function CatalogPage() {
  const token = useToken();
  const [items, setItems] = useState<CatalogItem[]>([]);
  const [name, setName] = useState("");
  const [category, setCategory] = useState("");
  const [summary, setSummary] = useState("");
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const refresh = () => {
    if (!token) return;
    listCatalog(token).then((r) => setItems(r.items)).catch((e) => setErr(e.message));
  };

  useEffect(() => {
    if (!token) return;
    listCatalog(token).then((r) => setItems(r.items)).catch((e) => setErr(e.message));
  }, [token]);

  const add = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!token) return;
    setBusy(true);
    setErr(null);
    try {
      await createCatalog(token, { name, category, summary });
      setName("");
      setCategory("");
      setSummary("");
      refresh();
    } catch (e) {
      setErr(e instanceof Error ? e.message : "failed");
    } finally {
      setBusy(false);
    }
  };

  const remove = async (id: string) => {
    if (!token) return;
    await deleteCatalog(token, id).catch(() => {});
    refresh();
  };

  return (
    <>
      <PageHeader title="Catalog" subtitle="Public catalog entries buyers can search and your agent can quote from." />

      <div className="card" id="add-item" style={{ marginBottom: "1.5rem" }}>
        <h2>Add item</h2>
        <form className="row wrap" onSubmit={add} style={{ marginTop: "0.75rem", alignItems: "flex-end" }}>
          <div className="field grow"><label>Name</label><input className="input" value={name} onChange={(e) => setName(e.target.value)} required /></div>
          <div className="field grow"><label>Category</label><input className="input" value={category} onChange={(e) => setCategory(e.target.value)} required /></div>
          <div className="field grow"><label>Summary</label><input className="input" value={summary} onChange={(e) => setSummary(e.target.value)} /></div>
          <button className="btn btn-primary" disabled={busy}>{busy ? "Adding…" : "Add"}</button>
        </form>
        {err && <p className="err" style={{ marginTop: "0.5rem" }}>{err}</p>}
      </div>

      <div className="card">
        <h2>Items ({items.length})</h2>
        <table className="tbl" style={{ marginTop: "0.5rem" }}>
          <thead><tr><th>Name</th><th>Category</th><th>Summary</th><th></th></tr></thead>
          <tbody>
            {items.map((it) => (
              <tr key={it.id}>
                <td>{it.name}</td>
                <td className="muted">{it.category}</td>
                <td className="muted small">{it.summary}</td>
                <td style={{ textAlign: "right" }}>
                  <button className="btn btn-sm btn-danger" onClick={() => remove(it.id)}>Delete</button>
                </td>
              </tr>
            ))}
            {items.length === 0 && (
              <tr><td colSpan={4}>
                <EmptyState
                  title="No catalog items yet"
                  body="Add products so buyers can find you and your agent can quote accurately."
                  cta={{ label: "Add your first item", href: "#add-item" }}
                />
              </td></tr>
            )}
          </tbody>
        </table>
        <p className="notice" style={{ marginTop: "1rem" }}>
          Tip: the seller agent can also bulk-import catalogs (PDF / XLSX / CSV) with embeddings from its own admin UI.
        </p>
      </div>
    </>
  );
}
