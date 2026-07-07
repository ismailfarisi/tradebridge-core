"use client";

import { useEffect, useState } from "react";
import { PageHeader } from "@/components/AppShell";
import { useToken } from "@/lib/auth";
import { getExtraction, updateExtraction, type ExtractionSettings } from "@/lib/api";

export default function ModelsPage() {
  const token = useToken();
  const [loaded, setLoaded] = useState<ExtractionSettings | null>(null);
  const [provider, setProvider] = useState("anthropic");
  const [model, setModel] = useState("");
  const [baseUrl, setBaseUrl] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [clearKey, setClearKey] = useState(false);
  const [msg, setMsg] = useState<string | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!token) return;
    getExtraction(token)
      .then((s) => {
        setLoaded(s);
        setProvider(s.provider || "anthropic");
        setModel(s.model);
        setBaseUrl(s.base_url);
      })
      .catch((e) => setErr(e.message));
  }, [token]);

  const save = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!token) return;
    setBusy(true);
    setMsg(null);
    setErr(null);
    try {
      const s = await updateExtraction(token, {
        provider,
        model,
        base_url: baseUrl,
        api_key: apiKey || undefined,
        clear_api_key: clearKey || undefined,
      });
      setLoaded(s);
      setApiKey("");
      setClearKey(false);
      setMsg("Saved.");
    } catch (e) {
      setErr(e instanceof Error ? e.message : "save failed");
    } finally {
      setBusy(false);
    }
  };

  const keyPlaceholder = loaded?.has_key ? `key set (…${loaded.key_last4}) — blank keeps it` : "paste API key";

  return (
    <>
      <PageHeader title="Model settings" subtitle="Provider + model that powers RFQ requirement extraction. Any OpenAI-compatible endpoint works." />

      <div className="card" style={{ maxWidth: 560 }}>
        <form className="col" onSubmit={save}>
          <div className="field">
            <label>Provider</label>
            <select className="select" value={provider} onChange={(e) => setProvider(e.target.value)}>
              <option value="anthropic">Anthropic (Claude)</option>
              <option value="openai_compatible">OpenAI-compatible (any model)</option>
            </select>
          </div>
          <div className="field">
            <label>Model</label>
            <input className="input" value={model} onChange={(e) => setModel(e.target.value)} placeholder={provider === "anthropic" ? "claude-haiku-4-5" : "gpt-4o-mini"} />
          </div>
          {provider === "openai_compatible" && (
            <div className="field">
              <label>Base URL</label>
              <input className="input" value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)} placeholder="https://api.openai.com/v1" />
            </div>
          )}
          <div className="field">
            <label>API key</label>
            <input className="input" type="password" value={apiKey} onChange={(e) => setApiKey(e.target.value)} placeholder={keyPlaceholder} />
          </div>
          {loaded?.has_key && (
            <label className="row small muted" style={{ gap: "0.5rem" }}>
              <input type="checkbox" checked={clearKey} onChange={(e) => setClearKey(e.target.checked)} /> Remove the stored key
            </label>
          )}
          {msg && <p className="ok">{msg}</p>}
          {err && <p className="err">{err}</p>}
          <div><button className="btn btn-primary" disabled={busy}>{busy ? "Saving…" : "Save"}</button></div>
        </form>
      </div>

      <p className="notice" style={{ maxWidth: 560, marginTop: "1rem" }}>
        Keys are stored encrypted at rest and never returned in plaintext. Settings apply to your
        company; if left blank, the platform&apos;s configured defaults are used. Per-provider
        routing policies (fallbacks, per-task models) are planned.
      </p>
    </>
  );
}
