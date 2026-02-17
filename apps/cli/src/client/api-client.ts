export interface ApiErrorEnvelope {
  error?: {
    code?: string;
    message?: string;
    details?: unknown;
  };
  [k: string]: unknown;
}

export class ApiClientError extends Error {
  readonly status: number;
  readonly code: string;
  readonly details?: unknown;
  readonly payload: unknown;

  constructor(input: { status: number; code: string; message: string; details?: unknown; payload: unknown }) {
    super(input.message);
    this.name = "ApiClientError";
    this.status = input.status;
    this.code = input.code;
    this.details = input.details;
    this.payload = input.payload;
  }
}

export interface ApiClientInit {
  base?: string;
  apiKey?: string;
}

const REQUEST_SOURCE_HEADER = "X-NextAI-Source";
const REQUEST_SOURCE_CLI = "cli";

function parseResponsePayload(text: string): unknown {
  try {
    return text ? JSON.parse(text) : {};
  } catch {
    return { raw: text };
  }
}

function toClientError(status: number, payload: unknown, fallbackMessage: string): ApiClientError {
  const envelope = payload as ApiErrorEnvelope;
  const code = envelope?.error?.code ?? "http_error";
  const message = envelope?.error?.message ?? fallbackMessage;
  return new ApiClientError({
    status,
    code,
    message,
    details: envelope?.error?.details,
    payload,
  });
}

export class ApiClient {
  private base: string;
  private apiKey: string;

  constructor(input?: string | ApiClientInit) {
    const init = typeof input === "string" ? { base: input } : input;
    this.base = (init?.base ?? process.env.NEXTAI_API_BASE ?? "http://127.0.0.1:8088").replace(/\/$/, "");
    this.apiKey = (init?.apiKey ?? process.env.NEXTAI_API_KEY ?? "").trim();
  }

  setBaseURL(base: string): void {
    const normalized = base.trim().replace(/\/$/, "");
    if (normalized) {
      this.base = normalized;
    }
  }

  getBaseURL(): string {
    return this.base;
  }

  setAPIKey(apiKey?: string): void {
    this.apiKey = (apiKey ?? "").trim();
  }

  getAPIKey(): string {
    return this.apiKey;
  }

  buildRequest(path: string, init?: RequestInit): { url: string; init: RequestInit } {
    const headers = new Headers(init?.headers ?? {});
    if (!headers.has("content-type")) {
      headers.set("content-type", "application/json");
    }
    if (!headers.has(REQUEST_SOURCE_HEADER)) {
      headers.set(REQUEST_SOURCE_HEADER, REQUEST_SOURCE_CLI);
    }
    if (this.apiKey !== "" && !headers.has("X-API-Key")) {
      headers.set("X-API-Key", this.apiKey);
    }
    return {
      url: `${this.base}${path}`,
      init: {
        ...init,
        headers,
      },
    };
  }

  async request<T>(path: string, init?: RequestInit): Promise<T> {
    const request = this.buildRequest(path, init);
    const res = await fetch(request.url, request.init);

    const text = await res.text();
    const data = parseResponsePayload(text);

    if (!res.ok) {
      throw toClientError(res.status, data, `${res.status} ${res.statusText}`.trim());
    }

    return data as T;
  }

  get<T>(path: string): Promise<T> {
    return this.request<T>(path);
  }

  post<T>(path: string, body?: unknown): Promise<T> {
    return this.request<T>(path, { method: "POST", body: body ? JSON.stringify(body) : undefined });
  }

  put<T>(path: string, body?: unknown): Promise<T> {
    return this.request<T>(path, { method: "PUT", body: body ? JSON.stringify(body) : undefined });
  }

  delete<T>(path: string): Promise<T> {
    return this.request<T>(path, { method: "DELETE" });
  }

  workspaceLs(): Promise<unknown> {
    return this.get("/workspace/files");
  }

  workspaceCat(path: string): Promise<unknown> {
    return this.get(`/workspace/files/${encodeURIComponent(path)}`);
  }

  workspacePut(path: string, payload: unknown): Promise<unknown> {
    return this.put(`/workspace/files/${encodeURIComponent(path)}`, payload);
  }

  workspaceRm(path: string): Promise<unknown> {
    return this.delete(`/workspace/files/${encodeURIComponent(path)}`);
  }

  workspaceExport(): Promise<unknown> {
    return this.get("/workspace/export");
  }

  workspaceImport(payload: unknown): Promise<unknown> {
    return this.post("/workspace/import", payload);
  }
}
