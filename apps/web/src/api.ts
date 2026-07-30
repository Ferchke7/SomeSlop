export class APIError extends Error {
  status: number;
  code: string;
  fieldErrors?: Record<string, string>;

  constructor(status: number, payload: { code?: string; message?: string; fieldErrors?: Record<string, string> }) {
    super(payload.message || "Не удалось выполнить запрос");
    this.status = status;
    this.code = payload.code || "unknown_error";
    this.fieldErrors = payload.fieldErrors;
  }
}

export async function api<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  if (init.body && !(init.body instanceof FormData) && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  const response = await fetch(`/api/v1${path}`, {
    ...init,
    headers,
    credentials: "include"
  });
  if (!response.ok) {
    let payload: { code?: string; message?: string; fieldErrors?: Record<string, string> } = {};
    try { payload = await response.json(); } catch { /* empty response */ }
    throw new APIError(response.status, payload);
  }
  if (response.status === 204) return undefined as T;
  return response.json() as Promise<T>;
}

export const jsonBody = (value: unknown): Pick<RequestInit, "body"> => ({
  body: JSON.stringify(value)
});

export async function ankiInvoke<T>(action: string, params: Record<string, unknown> = {}): Promise<T> {
  const response = await fetch("http://127.0.0.1:8765", {
    method: "POST",
    body: JSON.stringify({ action, version: 6, params }),
    headers: { "Content-Type": "application/json" }
  });
  const payload = await response.json() as { result: T; error: string | null };
  if (payload.error) throw new Error(payload.error);
  return payload.result;
}

export async function contentHash(value: unknown): Promise<string> {
  const bytes = new TextEncoder().encode(JSON.stringify(value));
  const digest = await crypto.subtle.digest("SHA-256", bytes);
  return Array.from(new Uint8Array(digest)).map((byte) => byte.toString(16).padStart(2, "0")).join("");
}

