"use client";

import { useEffect, useState } from "react";
import { PageHeader } from "@/components/AppShell";
import { useToken } from "@/lib/auth";
import { getProfile, upsertProfile } from "@/lib/api";

export default function ProfilePage() {
  const token = useToken();
  const [description, setDescription] = useState("");
  const [categories, setCategories] = useState("");
  const [location, setLocation] = useState("");
  const [msg, setMsg] = useState<string | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!token) return;
    getProfile(token)
      .then((p) => {
        if (!p) return;
        setDescription((p.description as string) ?? "");
        setCategories(((p.categories as string[]) ?? []).join(", "));
        setLocation((p.location as string) ?? "");
      })
      .catch(() => {});
  }, [token]);

  const save = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!token) return;
    setBusy(true);
    setMsg(null);
    setErr(null);
    try {
      await upsertProfile(token, {
        description,
        categories: categories.split(",").map((s) => s.trim()).filter(Boolean),
        location,
      });
      setMsg("Saved. Your profile is now discoverable and used for RFQ matching.");
    } catch (e) {
      setErr(e instanceof Error ? e.message : "save failed");
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <PageHeader title="Supplier profile" subtitle="Buyers find you by category; RFQ matching uses these fields." />
      <div className="card" style={{ maxWidth: 560 }}>
        <form className="col" onSubmit={save}>
          <div className="field">
            <label>Description</label>
            <textarea className="input" rows={3} value={description} onChange={(e) => setDescription(e.target.value)} placeholder="What you supply…" />
          </div>
          <div className="field">
            <label>Categories (comma-separated)</label>
            <input className="input" value={categories} onChange={(e) => setCategories(e.target.value)} placeholder="packaging, logistics" />
          </div>
          <div className="field">
            <label>Location</label>
            <input className="input" value={location} onChange={(e) => setLocation(e.target.value)} placeholder="Dubai, UAE" />
          </div>
          {msg && <p className="ok">{msg}</p>}
          {err && <p className="err">{err}</p>}
          <div><button className="btn btn-primary" disabled={busy}>{busy ? "Saving…" : "Save profile"}</button></div>
        </form>
      </div>
    </>
  );
}
