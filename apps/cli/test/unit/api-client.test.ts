import { describe, expect, it, vi } from "vitest";
import { ApiClient, ApiClientError } from "../../src/client/api-client.js";

describe("ApiClient", () => {
  it("throws when backend returns non-2xx", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ error: { code: "x", message: "bad" } }), { status: 400 })));
    const c = new ApiClient("http://127.0.0.1:8088");
    await expect(c.get("/healthz")).rejects.toBeInstanceOf(ApiClientError);
    await expect(c.get("/healthz")).rejects.toMatchObject({
      code: "x",
      message: "bad",
      status: 400,
    });
  });

  it("injects X-API-Key header when configured", async () => {
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({ ok: true }), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);

    const c = new ApiClient({
      base: "http://127.0.0.1:8088",
      apiKey: "secret-token",
    });
    await c.get("/healthz");

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const init = fetchMock.mock.calls[0]?.[1] as RequestInit;
    const headers = init.headers instanceof Headers ? init.headers : new Headers(init.headers);
    expect(headers.get("X-API-Key")).toBe("secret-token");
    expect(headers.get("X-NextAI-Source")).toBe("cli");
  });
});
