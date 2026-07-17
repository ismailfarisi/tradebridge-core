// The seller agent console is a separate app (its URL is deploy-configured via
// NEXT_PUBLIC_AGENT_URL, baked at build time). When unset, callers should
// degrade to on-page guidance instead of a broken link.
export const AGENT_CONSOLE_URL = process.env.NEXT_PUBLIC_AGENT_URL ?? "";

export function agentOnboardingHref(): string | null {
  return AGENT_CONSOLE_URL ? `${AGENT_CONSOLE_URL.replace(/\/$/, "")}/onboarding` : null;
}
