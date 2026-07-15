"use client";

import { useEffect, useState } from "react";
import AdminShell from "@/components/AdminShell";
import { getAdminSettings, updateAdminSettings, testAdminSettings, getAdminHealth, useAdminToken, type AdminSettings, type TestResult, type LlmHealth } from "@/lib/admin";

export default function AdminSettingsPage() {
  return (
    <AdminShell>
      <SettingsForm />
    </AdminShell>
  );
}

function SettingsForm() {
  const token = useAdminToken();
  const [s, setS] = useState<AdminSettings | null>(null);
  const [provider, setProvider] = useState("anthropic");
  const [model, setModel] = useState("");
  const [baseUrl, setBaseUrl] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [clearKey, setClearKey] = useState(false);
  const [exPrompt, setExPrompt] = useState("");
  const [inPrompt, setInPrompt] = useState("");
  const [msg, setMsg] = useState<string | null>(null);
  const [msgKind, setMsgKind] = useState<"ok" | "err">("ok");
  const [busy, setBusy] = useState(false);
  const [health, setHealth] = useState<LlmHealth | null>(null);
  const [test, setTest] = useState<TestResult | null>(null);
  const [testing, setTesting] = useState(false);

  useEffect(() => {
    if (!token) return;
    getAdminSettings(token).then((v) => {
      setS(v);
      setProvider(v.extraction.provider || "anthropic");
      setModel(v.extraction.model);
      setBaseUrl(v.extraction.base_url);
      setExPrompt(v.extraction_system_prompt);
      setInPrompt(v.intake_system_prompt);
    }).catch((e) => { setMsg(e.message); setMsgKind("err"); });
  }, [token]);

  useEffect(() => {
    if (!token) return;
    let alive = true;
    const tick = () => getAdminHealth(token).then((h) => { if (alive) setHealth(h); }).catch(() => {});
    tick();
    const id = setInterval(tick, 30000);
    return () => { alive = false; clearInterval(id); };
  }, [token]);

  const runTest = async () => {
    if (!token) return;
    setTesting(true); setTest(null);
    try { setTest(await testAdminSettings(token)); }
    catch (e) { setTest({ ok: false, model: "", latency_ms: 0, error: e instanceof Error ? e.message : "test failed" }); }
    finally { setTesting(false); getAdminHealth(token).then(setHealth).catch(() => {}); }
  };

  const save = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!token) return;
    setBusy(true); setMsg(null);
    try {
      const v = await updateAdminSettings(token, {
        provider, model, base_url: baseUrl,
        api_key: apiKey || undefined, clear_api_key: clearKey || undefined,
        extraction_system_prompt: exPrompt, intake_system_prompt: inPrompt,
      });
      setS(v); setApiKey(""); setClearKey(false);
      setMsg("Saved."); setMsgKind("ok");
    } catch (err) {
      setMsg(err instanceof Error ? err.message : "save failed"); setMsgKind("err");
    } finally { setBusy(false); }
  };

  if (!s) return <p className="muted">Loading…</p>;
  const keyPlaceholder = s.extraction.has_key ? `key set (…${s.extraction.key_last4}) — blank keeps it` : "paste API key";

  return (
    <form className="col" onSubmit={save} style={{ gap: "1.5rem" }}>
      {health?.degraded && (
        <div className="err" style={{ marginBottom: "1rem", padding: "0.75rem 1rem", borderRadius: 8 }}>
          ⚠️ <strong>AI is in scripted fallback mode.</strong> Recent model calls failed
          ({health.recent_fallbacks} of the last {health.window}).{" "}
          {health.last_error ? <>Last error: <code>{health.last_error}</code>.</> : null}{" "}
          Check the model settings below and use <strong>Test connection</strong>.
        </div>
      )}
      <div className="card">
        <h2>Model settings (global)</h2>
        <p className="muted small">Provider + model that power RFQ extraction and chat intake for every company.</p>
        <div className="field"><label>Provider</label>
          <select className="select" value={provider} onChange={(e) => setProvider(e.target.value)}>
            <option value="anthropic">Anthropic (Claude)</option>
            <option value="openai_compatible">OpenAI-compatible</option>
          </select>
        </div>
        <div className="field"><label>Model</label>
          <input className="input" value={model} onChange={(e) => setModel(e.target.value)} placeholder={provider === "anthropic" ? "claude-haiku-4-5" : "gpt-4o-mini"} />
        </div>
        {provider === "openai_compatible" && (
          <div className="field"><label>Base URL</label>
            <input className="input" value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)} placeholder="https://api.openai.com/v1" />
          </div>
        )}
        <div className="field"><label>API key</label>
          <input className="input" type="password" value={apiKey} onChange={(e) => setApiKey(e.target.value)} placeholder={keyPlaceholder} />
        </div>
        {s.extraction.has_key && (
          <label className="row small muted" style={{ gap: "0.5rem" }}>
            <input type="checkbox" checked={clearKey} onChange={(e) => setClearKey(e.target.checked)} /> Remove the stored key
          </label>
        )}
      </div>

      <div className="card">
        <h2>System prompts</h2>
        <div className="field">
          <label>Extraction prompt <button type="button" className="btn btn-sm btn-ghost" onClick={() => setExPrompt("")}>Reset to default</button></label>
          <textarea className="input" rows={6} value={exPrompt} onChange={(e) => setExPrompt(e.target.value)} placeholder={s.default_extraction_prompt} />
          <p className="muted small">Blank = use the built-in default.</p>
        </div>
        <div className="field">
          <label>Chat intake prompt <button type="button" className="btn btn-sm btn-ghost" onClick={() => setInPrompt("")}>Reset to default</button></label>
          <textarea className="input" rows={6} value={inPrompt} onChange={(e) => setInPrompt(e.target.value)} placeholder={s.default_intake_prompt} />
          <p className="muted small">Blank = use the built-in default.</p>
        </div>
      </div>

      {msg && <p className={msgKind}>{msg}</p>}
      <div className="row" style={{ gap: "0.5rem", alignItems: "center" }}>
        <button className="btn btn-primary" disabled={busy}>{busy ? "Saving…" : "Save"}</button>
        <button type="button" className="btn btn-ghost" disabled={testing || busy} onClick={runTest}>
          {testing ? "Testing…" : "Test connection"}
        </button>
        {test && (test.ok
          ? <span className="ok small">✅ Connected — {test.model} ({test.latency_ms} ms)</span>
          : <span className="err small">❌ {test.error}</span>)}
      </div>
    </form>
  );
}
