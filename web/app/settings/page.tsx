"use client";

import { useEffect, useState, useSyncExternalStore } from "react";
import {
  clearToken,
  getExtractionSettings,
  login,
  setToken,
  subscribeToken,
  tokenServerSnapshot,
  tokenSnapshot,
  updateExtractionSettings,
  type ExtractionSettings,
} from "@/lib/api";

const card: React.CSSProperties = {
  width: "100%",
  maxWidth: 520,
  border: "1px solid #e4e4e7",
  borderRadius: 12,
  padding: "1.5rem",
  display: "flex",
  flexDirection: "column",
  gap: "0.9rem",
  background: "#fff",
};
const label: React.CSSProperties = { fontSize: "0.8rem", fontWeight: 600, color: "#52525b" };
const input: React.CSSProperties = {
  padding: "0.55rem 0.7rem",
  border: "1px solid #d4d4d8",
  borderRadius: 8,
  fontSize: "0.95rem",
  width: "100%",
};
const button: React.CSSProperties = {
  padding: "0.6rem 1rem",
  border: "none",
  borderRadius: 8,
  background: "#111827",
  color: "#fff",
  fontWeight: 600,
  cursor: "pointer",
};

function Field({ children, name }: { children: React.ReactNode; name: string }) {
  return (
    <label style={{ display: "flex", flexDirection: "column", gap: "0.35rem" }}>
      <span style={label}>{name}</span>
      {children}
    </label>
  );
}

export default function SettingsPage() {
  const token = useSyncExternalStore(
    subscribeToken,
    tokenSnapshot,
    tokenServerSnapshot,
  );

  return (
    <main
      style={{
        minHeight: "100vh",
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
        fontFamily: "system-ui, sans-serif",
        gap: "1.25rem",
        padding: "2rem",
        background: "#fafafa",
      }}
    >
      <h1 style={{ fontSize: "1.5rem", fontWeight: 700 }}>Provider Settings</h1>
      <p style={{ color: "#71717a", marginTop: "-0.6rem", fontSize: "0.9rem" }}>
        RFQ requirement extraction — configure the model and API key for your company.
      </p>
      {token ? (
        <SettingsForm token={token} onLogout={() => clearToken()} />
      ) : (
        <LoginForm onLogin={(t) => setToken(t)} />
      )}
    </main>
  );
}

function LoginForm({ onLogin }: { onLogin: (t: string) => void }) {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    try {
      onLogin(await login(email, password));
    } catch (e) {
      setErr(e instanceof Error ? e.message : "login failed");
    } finally {
      setBusy(false);
    }
  };

  return (
    <form style={card} onSubmit={submit}>
      <Field name="Email">
        <input style={input} type="email" value={email} onChange={(e) => setEmail(e.target.value)} required />
      </Field>
      <Field name="Password">
        <input style={input} type="password" value={password} onChange={(e) => setPassword(e.target.value)} required />
      </Field>
      {err && <p style={{ color: "#dc2626", fontSize: "0.85rem" }}>{err}</p>}
      <button style={button} disabled={busy}>{busy ? "Signing in…" : "Sign in"}</button>
    </form>
  );
}

function SettingsForm({ token, onLogout }: { token: string; onLogout: () => void }) {
  const [loaded, setLoaded] = useState<ExtractionSettings | null>(null);
  const [provider, setProvider] = useState("anthropic");
  const [model, setModel] = useState("");
  const [baseURL, setBaseURL] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [clearKey, setClearKey] = useState(false);
  const [msg, setMsg] = useState<string | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    getExtractionSettings(token)
      .then((s) => {
        setLoaded(s);
        setProvider(s.provider || "anthropic");
        setModel(s.model);
        setBaseURL(s.base_url);
      })
      .catch((e) => setErr(e instanceof Error ? e.message : "load failed"));
  }, [token]);

  const save = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setMsg(null);
    setErr(null);
    try {
      const s = await updateExtractionSettings(token, {
        provider,
        model,
        base_url: baseURL,
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

  const keyPlaceholder = loaded?.has_key
    ? `key set (…${loaded.key_last4}) — leave blank to keep`
    : "paste API key";

  return (
    <form style={card} onSubmit={save}>
      <Field name="Provider">
        <select style={input} value={provider} onChange={(e) => setProvider(e.target.value)}>
          <option value="anthropic">Anthropic (Claude)</option>
          <option value="openai_compatible">OpenAI-compatible (any model)</option>
        </select>
      </Field>
      <Field name="Model">
        <input
          style={input}
          value={model}
          onChange={(e) => setModel(e.target.value)}
          placeholder={provider === "anthropic" ? "claude-haiku-4-5" : "gpt-4o-mini"}
        />
      </Field>
      {provider === "openai_compatible" && (
        <Field name="Base URL">
          <input
            style={input}
            value={baseURL}
            onChange={(e) => setBaseURL(e.target.value)}
            placeholder="https://api.openai.com/v1"
          />
        </Field>
      )}
      <Field name="API Key">
        <input
          style={input}
          type="password"
          value={apiKey}
          onChange={(e) => setApiKey(e.target.value)}
          placeholder={keyPlaceholder}
        />
      </Field>
      {loaded?.has_key && (
        <label style={{ display: "flex", alignItems: "center", gap: "0.5rem", fontSize: "0.85rem", color: "#52525b" }}>
          <input type="checkbox" checked={clearKey} onChange={(e) => setClearKey(e.target.checked)} />
          Remove the stored key
        </label>
      )}
      {msg && <p style={{ color: "#16a34a", fontSize: "0.85rem" }}>{msg}</p>}
      {err && <p style={{ color: "#dc2626", fontSize: "0.85rem" }}>{err}</p>}
      <div style={{ display: "flex", gap: "0.6rem" }}>
        <button style={button} disabled={busy}>{busy ? "Saving…" : "Save"}</button>
        <button
          type="button"
          onClick={onLogout}
          style={{ ...button, background: "#fff", color: "#111827", border: "1px solid #d4d4d8" }}
        >
          Sign out
        </button>
      </div>
    </form>
  );
}
