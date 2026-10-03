export type Json = any;

let csrfToken = "";
export const setCsrf = (t: string) => (csrfToken = t);
export const getCsrf = () => csrfToken;

let onUnauthorized: () => void = () => {};
export const setOnUnauthorized = (f: () => void) => (onUnauthorized = f);

export async function api<T = Json>(path: string, init?: RequestInit): Promise<T> {
  const headers: Record<string, string> = { ...(init?.headers as Record<string, string>) };
  if (init?.body) headers["content-type"] = "application/json";
  const method = (init?.method ?? "GET").toUpperCase();
  if (method !== "GET" && method !== "HEAD" && csrfToken) headers["X-CSRF-Token"] = csrfToken;
  const r = await fetch(path, { credentials: "same-origin", ...init, headers });
  if (r.status === 401) {
    onUnauthorized();
    throw new Error("401 unauthorized");
  }
  const text = await r.text();
  try {
    return text ? (JSON.parse(text) as T) : ({} as T);
  } catch {
    throw new Error(`${r.status} ${text.slice(0, 200)}`);
  }
}

export const post = <T = Json>(path: string, body?: unknown) =>
  api<T>(path, { method: "POST", body: JSON.stringify(body ?? {}) });
export const put = <T = Json>(path: string, body: unknown) =>
  api<T>(path, { method: "PUT", body: JSON.stringify(body) });

export const num = (n: number | undefined | null) =>
  n === undefined || n === null ? "–" : n >= 1e9 ? (n / 1e9).toFixed(1) + "B" : n >= 1e6 ? (n / 1e6).toFixed(1) + "M" : n >= 1e3 ? (n / 1e3).toFixed(1) + "k" : String(n);

export const ms = (n: number | undefined | null) =>
  n === undefined || n === null ? "–" : n >= 1000 ? (n / 1000).toFixed(n >= 10000 ? 0 : 1) + "s" : n + "ms";

export const ago = (iso: string | number | undefined | null) => {
  if (!iso) return "–";
  const t = typeof iso === "number" ? iso : Date.parse(iso);
  if (Number.isNaN(t)) return "–";
  const d = Math.max(0, Math.round((Date.now() - t) / 1000));
  if (d < 60) return d + "s ago";
  if (d < 3600) return Math.round(d / 60) + "m ago";
  if (d < 86400) return Math.round(d / 3600) + "h ago";
  return Math.round(d / 86400) + "d ago";
};

export const clock = (t: number | string) =>
  new Date(typeof t === "number" ? t : Date.parse(t)).toLocaleTimeString([], { hour12: false });

export function sparkline(points: number[], w = 120, h = 28): string {
  if (!points.length) return "";
  const max = Math.max(1, ...points);
  const step = points.length > 1 ? w / (points.length - 1) : w;
  return points
    .map((p, i) => `${i === 0 ? "M" : "L"}${(i * step).toFixed(1)},${(h - (p / max) * (h - 2) - 1).toFixed(1)}`)
    .join(" ");
}

/** ConfigView -> ConfigUpdate: secrets stay referenced by fingerprint id. */
export function configToUpdate(v: any) {
  const secrets = (arr: any[]) => (arr ?? []).map((s) => (s.value ? { value: s.value } : { id: s.id }));
  return {
    listen: v.listen,
    server_keys: secrets(v.server_keys),
    zen_keys: secrets(v.zen_keys),
    go_keys: secrets(v.go_keys),
    anonymous: v.anonymous,
    proxies: secrets(v.proxies),
    proxyfile: v.proxyfile,
    upstream: v.upstream,
    retry: v.retry,
    models: v.models,
    performance: v.performance,
    logging: v.logging,
    prefer: v.prefer,
    reasoning: v.reasoning,
    webui: {
      enabled: v.webui.enabled,
      listen: v.webui.listen,
      username: v.webui.username,
      session_ttl_minutes: v.webui.session_ttl_minutes,
    },
  };
}

/** Append a new secret value (plaintext) to a ConfigView secret list. */
export function withNewSecret(arr: any[], value: string) {
  return [...(arr ?? []), { value }];
}

export const EFFORTS = ["", "minimal", "low", "medium", "high", "xhigh", "max", "none"] as const;

/** Extract the assistant reply text from a chat/responses/anthropic response body. */
export function replyText(resp: any): string {
  if (resp == null) return "";
  if (typeof resp === "string") return resp;
  const chat = resp.choices?.[0]?.message;
  if (chat) return chat.content ?? chat.reasoning_content ?? "";
  if (typeof resp.output_text === "string") return resp.output_text;
  if (Array.isArray(resp.output)) {
    for (const item of resp.output) {
      const parts = item?.content;
      if (Array.isArray(parts)) {
        const t = parts.map((p: any) => p.text ?? p.output_text ?? "").join("");
        if (t) return t;
      }
    }
  }
  if (Array.isArray(resp.content)) {
    return resp.content.filter((b: any) => b?.type === "text").map((b: any) => b.text).join("");
  }
  if (resp.error) {
    return typeof resp.error === "string" ? resp.error : (resp.error.message ?? JSON.stringify(resp.error));
  }
  return "";
}

/** Pick the small set of useful metrics out of an inference result. */
export function replyMeta(out: any): { label: string; value: string }[] {
  if (!out) return [];
  const items: { label: string; value: string }[] = [];
  if (out.http_status) items.push({ label: "status", value: String(out.http_status) });
  if (out.duration_ms !== undefined) items.push({ label: "time", value: ms(out.duration_ms) });
  if (out.route?.tier) items.push({ label: "source", value: out.route.tier === "zen" ? "opencode" : out.route.tier === "go" ? "cline" : out.route.tier });
  if (out.route?.channel) items.push({ label: "channel", value: out.route.channel });
  if (out.key_test) items.push({ label: "key", value: out.key_test });
  const u = out.response?.usage;
  if (u) {
    const inn = u.prompt_tokens ?? u.input_tokens;
    const outt = u.completion_tokens ?? u.output_tokens;
    if (inn !== undefined) items.push({ label: "in", value: num(inn) });
    if (outt !== undefined) items.push({ label: "out", value: num(outt) });
  }
  if (out.request_id) items.push({ label: "req", value: String(out.request_id).slice(0, 8) });
  return items;
}
