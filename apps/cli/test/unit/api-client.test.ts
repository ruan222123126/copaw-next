import { describe, expect, it, vi } from "vitest";
import { ApiClient } from "../../src/client/api-client.js";

describe("ApiClient", () => {
  it("throws when backend returns non-2xx", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ error: { code: "x", message: "bad" } }), { status: 400 })));
    const c = new ApiClient("http://127.0.0.1:8088");
    await expect(c.get("/healthz")).rejects.toThrow();
  });
});
