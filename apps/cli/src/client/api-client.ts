export class ApiClient {
  private readonly base: string;

  constructor(base?: string) {
    this.base = (base ?? process.env.COPAW_API_BASE ?? "http://127.0.0.1:8088").replace(/\/$/, "");
  }

  async request<T>(path: string, init?: RequestInit): Promise<T> {
    const res = await fetch(`${this.base}${path}`, {
      ...init,
      headers: {
        "content-type": "application/json",
        ...(init?.headers ?? {}),
      },
    });

    const text = await res.text();
    let data: unknown = {};
    try {
      data = text ? JSON.parse(text) : {};
    } catch {
      data = { raw: text };
    }

    if (!res.ok) {
      throw new Error(JSON.stringify(data));
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

  async uploadWorkspace(filePath: string): Promise<unknown> {
    const fs = await import("node:fs/promises");
    const form = new FormData();
    const data = await fs.readFile(filePath);
    const blob = new Blob([data], { type: "application/zip" });
    form.append("file", blob, "workspace.zip");
    const res = await fetch(`${this.base}/workspace/upload`, {
      method: "POST",
      body: form,
    });
    const json = await res.json();
    if (!res.ok) {
      throw new Error(JSON.stringify(json));
    }
    return json;
  }
}
