// @vitest-environment jsdom

import { readFileSync } from "node:fs";
import { join } from "node:path";

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: {
      "content-type": "application/json",
    },
  });
}

async function waitFor(condition: () => boolean, timeoutMS = 2000): Promise<void> {
  const startedAt = Date.now();
  while (!condition()) {
    if (Date.now() - startedAt > timeoutMS) {
      throw new Error("timeout waiting for condition");
    }
    await new Promise((resolve) => setTimeout(resolve, 10));
  }
}

describe("web e2e: set active model then send chat", () => {
  let originalFetch: typeof globalThis.fetch;

  beforeEach(() => {
    vi.resetModules();
    document.documentElement.innerHTML = readFileSync(join(process.cwd(), "src/index.html"), "utf8").replace(
      /<!doctype html>/i,
      "",
    );
    window.localStorage.clear();
    window.localStorage.setItem("nextai.web.locale", "zh-CN");
    originalFetch = globalThis.fetch;
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
    document.documentElement.innerHTML = "<head></head><body></body>";
  });

  it("在 Web 中设置 active model 后发送消息，回复不走 Echo 兜底", async () => {
    const replies = {
      model: "这是模型回复，不是 Echo",
    };
    const providerID = "openai-compatible";
    const modelID = "ark-code-latest";

    let processRequestSessionID = "";
    let processRequestUserID = "";
    let processRequestChannel = "";
    let processCalled = false;
    let activeSetCalled = false;

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/chats" && method === "GET") {
        if (!processCalled) {
          return jsonResponse([]);
        }
        return jsonResponse([
          {
            id: "chat-e2e-1",
            name: "你好",
            session_id: processRequestSessionID,
            user_id: processRequestUserID,
            channel: processRequestChannel,
            created_at: "2026-02-16T06:00:00Z",
            updated_at: "2026-02-16T06:00:10Z",
            meta: {},
          },
        ]);
      }

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: [
            {
              id: providerID,
              name: "OPENAI-COMPATIBLE",
              display_name: "ruan",
              openai_compatible: true,
              api_key_prefix: "OPENAI_COMPATIBLE_API_KEY",
              models: [{ id: modelID, name: modelID }],
              allow_custom_base_url: true,
              enabled: true,
              has_api_key: true,
              current_api_key: "skm***123",
              current_base_url: "https://example.com/v1",
            },
          ],
          provider_types: [
            { id: "openai", display_name: "openai" },
            { id: "openai-compatible", display_name: "openai Compatible" },
          ],
          defaults: {
            [providerID]: modelID,
          },
          active_llm: {
            provider_id: "",
            model: "",
          },
        });
      }

      if (url.pathname === "/models/active" && method === "PUT") {
        activeSetCalled = true;
        const payload = JSON.parse(String(init?.body ?? "{}")) as { provider_id?: string; model?: string };
        expect(payload.provider_id).toBe(providerID);
        expect(payload.model).toBe(modelID);
        return jsonResponse({
          active_llm: {
            provider_id: providerID,
            model: modelID,
          },
        });
      }

      if (url.pathname === "/agent/process" && method === "POST") {
        processCalled = true;
        const payload = JSON.parse(String(init?.body ?? "{}")) as {
          session_id?: string;
          user_id?: string;
          channel?: string;
        };
        processRequestSessionID = payload.session_id ?? "";
        processRequestUserID = payload.user_id ?? "";
        processRequestChannel = payload.channel ?? "";

        const sse = [
          `data: ${JSON.stringify({ type: "step_started", step: 1 })}`,
          `data: ${JSON.stringify({ type: "assistant_delta", step: 1, delta: replies.model })}`,
          `data: ${JSON.stringify({ type: "completed", step: 1, reply: replies.model })}`,
          "data: [DONE]",
          "",
        ].join("\n\n");
        return new Response(sse, {
          status: 200,
          headers: {
            "content-type": "text/event-stream",
          },
        });
      }

      if (url.pathname === "/chats/chat-e2e-1" && method === "GET") {
        return jsonResponse({
          messages: [
            {
              id: "msg-user",
              role: "user",
              type: "message",
              content: [{ type: "text", text: "你好" }],
            },
            {
              id: "msg-assistant",
              role: "assistant",
              type: "message",
              content: [{ type: "text", text: replies.model }],
            },
          ],
        });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    const modelsTabButton = document.querySelector<HTMLButtonElement>('button[data-tab="models"]');
    expect(modelsTabButton).not.toBeNull();
    modelsTabButton?.click();

    await waitFor(() => {
      const select = document.getElementById("models-active-provider-select") as HTMLSelectElement | null;
      return Boolean(select && select.options.length > 0 && select.value === providerID);
    });

    const modelsActiveForm = document.getElementById("models-active-form") as HTMLFormElement;
    modelsActiveForm.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));

    await waitFor(() => activeSetCalled);

    const chatTabButton = document.querySelector<HTMLButtonElement>('button[data-tab="chat"]');
    expect(chatTabButton).not.toBeNull();
    chatTabButton?.click();

    const messageInput = document.getElementById("message-input") as HTMLTextAreaElement;
    const composerForm = document.getElementById("composer") as HTMLFormElement;
    messageInput.value = "你好";
    composerForm.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));

    await waitFor(() => {
      const messages = Array.from(document.querySelectorAll<HTMLLIElement>("#message-list .message.assistant"));
      return messages.some((item) => item.textContent?.includes(replies.model));
    }, 4000);

    const assistantMessages = Array.from(document.querySelectorAll<HTMLLIElement>("#message-list .message.assistant"));
    expect(assistantMessages.length).toBeGreaterThan(0);
    const text = assistantMessages[assistantMessages.length - 1]?.textContent ?? "";
    expect(text).toContain(replies.model);
    expect(text).not.toContain("Echo:");
    expect(processCalled).toBe(true);
  });

  it("添加 openai-compatible 提供商时不会覆盖同类型已有配置", async () => {
    const existingProviderID = "openai-compatible";
    const modelID = "ark-code-latest";
    const catalogProviders = [
      {
        id: existingProviderID,
        name: "OPENAI-COMPATIBLE",
        display_name: "已有 Provider",
        openai_compatible: true,
        api_key_prefix: "OPENAI_COMPATIBLE_API_KEY",
        models: [{ id: modelID, name: modelID }],
        allow_custom_base_url: true,
        enabled: true,
        has_api_key: true,
        current_api_key: "skm***123",
        current_base_url: "https://example.com/v1",
      },
    ];

    let configuredProviderID = "";
    let overwroteExisting = false;

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/chats" && method === "GET") {
        return jsonResponse([]);
      }

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: catalogProviders,
          provider_types: [
            { id: "openai", display_name: "openai" },
            { id: "openai-compatible", display_name: "openai Compatible" },
          ],
          defaults: Object.fromEntries(catalogProviders.map((provider) => [provider.id, modelID])),
          active_llm: {
            provider_id: existingProviderID,
            model: modelID,
          },
        });
      }

      if (url.pathname.startsWith("/models/") && url.pathname.endsWith("/config") && method === "PUT") {
        const rawProviderID = url.pathname.slice("/models/".length, url.pathname.length - "/config".length);
        configuredProviderID = decodeURIComponent(rawProviderID);
        const exists = catalogProviders.some((provider) => provider.id === configuredProviderID);
        if (exists) {
          overwroteExisting = true;
        } else {
          catalogProviders.push({
            id: configuredProviderID,
            name: configuredProviderID.toUpperCase(),
            display_name: configuredProviderID,
            openai_compatible: true,
            api_key_prefix: "OPENAI_COMPATIBLE_API_KEY",
            models: [{ id: modelID, name: modelID }],
            allow_custom_base_url: true,
            enabled: true,
            has_api_key: false,
            current_api_key: "",
            current_base_url: "",
          });
        }
        return jsonResponse(catalogProviders.find((provider) => provider.id === configuredProviderID) ?? {}, 200);
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    const modelsTabButton = document.querySelector<HTMLButtonElement>('button[data-tab="models"]');
    expect(modelsTabButton).not.toBeNull();
    modelsTabButton?.click();

    await waitFor(() =>
      Boolean(
        document.querySelector<HTMLButtonElement>(
          `button[data-provider-action="edit"][data-provider-id="${existingProviderID}"]`,
        ),
      ),
    );

    const addProviderButton = document.getElementById("models-add-provider-btn") as HTMLButtonElement;
    addProviderButton.click();

    await waitFor(() => {
      const modal = document.getElementById("models-provider-modal");
      return Boolean(modal && !modal.classList.contains("is-hidden"));
    });

    const providerTypeSelect = document.getElementById("models-provider-type-select") as HTMLSelectElement;
    providerTypeSelect.value = "openai-compatible";
    providerTypeSelect.dispatchEvent(new Event("change", { bubbles: true }));

    const providerNameInput = document.getElementById("models-provider-name-input") as HTMLInputElement;
    providerNameInput.value = "";

    const providerForm = document.getElementById("models-provider-form") as HTMLFormElement;
    providerForm.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));

    await waitFor(() => configuredProviderID !== "");

    expect(configuredProviderID).toBe("openai-compatible-2");
    expect(overwroteExisting).toBe(false);
    expect(catalogProviders.map((provider) => provider.id)).toContain(existingProviderID);
    expect(catalogProviders.map((provider) => provider.id)).toContain("openai-compatible-2");
  });
});
