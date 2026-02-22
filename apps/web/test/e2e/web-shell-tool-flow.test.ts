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

describe("web e2e: /shell command sends biz_params.tool", () => {
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

  it("发送 /shell 命令时会携带 tool 调用参数", async () => {
    let processCalled = false;
    let sessionID = "";
    let userID = "";
    let channel = "";
    let capturedCommand = "";

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: [],
          provider_types: [],
          defaults: {},
          active_llm: {
            provider_id: "",
            model: "",
          },
        });
      }

      if (url.pathname === "/chats" && method === "GET") {
        if (!processCalled) {
          return jsonResponse([]);
        }
        return jsonResponse([
          {
            id: "chat-shell-1",
            name: "shell",
            session_id: sessionID,
            user_id: userID,
            channel,
            created_at: "2026-02-16T12:00:00Z",
            updated_at: "2026-02-16T12:00:10Z",
            meta: {},
          },
        ]);
      }

      if (url.pathname === "/agent/process" && method === "POST") {
        processCalled = true;
        const payload = JSON.parse(String(init?.body ?? "{}")) as {
          session_id?: string;
          user_id?: string;
          channel?: string;
          biz_params?: {
            tool?: {
              name?: string;
              input?: { command?: string };
            };
          };
        };
        sessionID = payload.session_id ?? "";
        userID = payload.user_id ?? "";
        channel = payload.channel ?? "";
        capturedCommand = payload.biz_params?.tool?.input?.command ?? "";
        expect(payload.biz_params?.tool?.name).toBe("shell");

        const sse = [
          `data: ${JSON.stringify({ type: "step_started", step: 1 })}`,
          `data: ${JSON.stringify({ type: "tool_call", step: 1, tool_call: { name: "shell", input: { command: capturedCommand } } })}`,
          `data: ${JSON.stringify({ type: "tool_result", step: 1, tool_result: { name: "shell", ok: true, summary: "done" } })}`,
          `data: ${JSON.stringify({ type: "assistant_delta", step: 1, delta: "shell done" })}`,
          `data: ${JSON.stringify({ type: "completed", step: 1, reply: "shell done" })}`,
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

      if (url.pathname === "/chats/chat-shell-1" && method === "GET") {
        const rawToolCall = JSON.stringify({
          type: "tool_call",
          step: 1,
          tool_call: {
            name: "shell",
            input: {
              command: capturedCommand,
            },
          },
        });
        return jsonResponse({
          messages: [
            {
              id: "msg-user",
              role: "user",
              type: "message",
              content: [{ type: "text", text: "/shell printf hello" }],
            },
            {
              id: "msg-assistant",
              role: "assistant",
              type: "message",
              content: [{ type: "text", text: "shell done" }],
              metadata: {
                tool_call_notices: [{ raw: rawToolCall }],
                tool_order: 2,
                text_order: 4,
              },
            },
          ],
        });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    const messageInput = document.getElementById("message-input") as HTMLTextAreaElement;
    messageInput.value = "/shell printf hello";
    messageInput.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));

    await waitFor(() => processCalled, 4000);
    expect(sessionID).toMatch(/^[a-z0-9]{6}$/);
    expect(capturedCommand).toBe("printf hello");

    await waitFor(() => {
      const messages = Array.from(document.querySelectorAll<HTMLLIElement>("#message-list .message.assistant"));
      return messages.some(
        (item) => {
          const summary = item.querySelector<HTMLElement>(".tool-call-summary")?.textContent ?? "";
          const raw = item.querySelector<HTMLElement>(".tool-call-raw")?.textContent ?? "";
          const firstClass = item.firstElementChild?.className ?? "";
          return (
            item.textContent?.includes("shell done") &&
            summary.includes("bash printf hello") &&
            raw.includes("done") &&
            !raw.includes('"command":"printf hello"') &&
            firstClass === "tool-call-list"
          );
        },
      );
    }, 4000);

    const chatItemButton = document.querySelector<HTMLButtonElement>("#chat-list .chat-item-btn");
    chatItemButton?.click();

    await waitFor(() => {
      const assistant = document.querySelector<HTMLLIElement>("#message-list .message.assistant:last-child");
      if (!assistant) {
        return false;
      }
      const firstClass = assistant.firstElementChild?.className ?? "";
      const summary = assistant.querySelector<HTMLElement>(".tool-call-summary")?.textContent ?? "";
      const raw = assistant.querySelector<HTMLElement>(".tool-call-raw")?.textContent ?? "";
      return firstClass === "tool-call-list" && summary.includes("bash printf hello") && !raw.includes('"command":"printf hello"');
    }, 4000);
  });

  it("开启 prompt 模板后，/prompts 命令会先展开再发送", async () => {
    window.localStorage.setItem("nextai.feature.prompt_templates", "true");
    let processCalled = false;
    let sessionID = "";
    let userID = "";
    let channel = "";
    let expandedText = "";
    let capturedCommand = "";

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: [],
          provider_types: [],
          defaults: {},
          active_llm: {
            provider_id: "",
            model: "",
          },
        });
      }

      if (url.pathname === "/chats" && method === "GET") {
        if (!processCalled) {
          return jsonResponse([]);
        }
        return jsonResponse([
          {
            id: "chat-template-1",
            name: "template",
            session_id: sessionID,
            user_id: userID,
            channel,
            created_at: "2026-02-16T12:00:00Z",
            updated_at: "2026-02-16T12:00:10Z",
            meta: {},
          },
        ]);
      }

      if (url.pathname === `/workspace/files/${encodeURIComponent("prompts/quick-task.md")}` && method === "GET") {
        return jsonResponse({ content: "/shell $CMD" });
      }

      if (url.pathname.startsWith("/workspace/files/") && method === "GET") {
        return jsonResponse({ content: "" });
      }

      if (url.pathname === "/agent/process" && method === "POST") {
        processCalled = true;
        const payload = JSON.parse(String(init?.body ?? "{}")) as {
          session_id?: string;
          user_id?: string;
          channel?: string;
          input?: Array<{
            content?: Array<{ text?: string }>;
          }>;
          biz_params?: {
            tool?: {
              name?: string;
              input?: { command?: string };
            };
          };
        };
        sessionID = payload.session_id ?? "";
        userID = payload.user_id ?? "";
        channel = payload.channel ?? "";
        expandedText = payload.input?.[0]?.content?.[0]?.text ?? "";
        capturedCommand = payload.biz_params?.tool?.input?.command ?? "";
        const sse = [
          `data: ${JSON.stringify({ type: "assistant_delta", step: 1, delta: "ok" })}`,
          `data: ${JSON.stringify({ type: "completed", step: 1, reply: "ok" })}`,
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

      if (url.pathname === "/chats/chat-template-1" && method === "GET") {
        return jsonResponse({
          messages: [
            {
              id: "msg-user",
              role: "user",
              type: "message",
              content: [{ type: "text", text: expandedText }],
            },
            {
              id: "msg-assistant",
              role: "assistant",
              type: "message",
              content: [{ type: "text", text: "ok" }],
            },
          ],
        });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    const messageInput = document.getElementById("message-input") as HTMLTextAreaElement;
    messageInput.value = "/prompts:quick-task CMD=printf_hello";
    messageInput.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));

    await waitFor(() => processCalled, 4000);
    expect(expandedText).toBe("/shell printf_hello");
    expect(capturedCommand).toBe("printf_hello");
  });

  it("prompt 模板缺少必填参数时会阻断发送", async () => {
    window.localStorage.setItem("nextai.feature.prompt_templates", "true");
    let processCalled = false;

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: [],
          provider_types: [],
          defaults: {},
          active_llm: {
            provider_id: "",
            model: "",
          },
        });
      }

      if (url.pathname === "/chats" && method === "GET") {
        return jsonResponse([]);
      }

      if (url.pathname === `/workspace/files/${encodeURIComponent("prompts/quick-task.md")}` && method === "GET") {
        return jsonResponse({ content: "hello $NAME" });
      }

      if (url.pathname.startsWith("/workspace/files/") && method === "GET") {
        return jsonResponse({ content: "" });
      }

      if (url.pathname === "/agent/process" && method === "POST") {
        processCalled = true;
        return new Response("unexpected", { status: 500 });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    const messageInput = document.getElementById("message-input") as HTMLTextAreaElement;
    const statusLine = document.getElementById("status-line") as HTMLElement;
    messageInput.value = "/prompts:quick-task";
    messageInput.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));

    await waitFor(() => (statusLine.textContent ?? "").includes("missing prompt arguments"), 4000);
    expect(processCalled).toBe(false);
  });

  it("可在设置中切换 prompt context introspect 开关", async () => {
    let systemLayersRequested = 0;
    let workspacePromptFileReads = 0;

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/runtime-config" && method === "GET") {
        return jsonResponse({
          features: {
            prompt_templates: false,
            prompt_context_introspect: false,
          },
        });
      }

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: [],
          provider_types: [],
          defaults: {},
          active_llm: {
            provider_id: "",
            model: "",
          },
        });
      }

      if (url.pathname === "/chats" && method === "GET") {
        return jsonResponse([]);
      }

      if (url.pathname === "/agent/system-layers" && method === "GET") {
        systemLayersRequested += 1;
        return jsonResponse({
          version: "v1",
          layers: [],
          estimated_tokens_total: 123,
        });
      }

      if (url.pathname.startsWith("/workspace/files/") && method === "GET") {
        workspacePromptFileReads += 1;
        return jsonResponse({ content: "" });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    await waitFor(() => workspacePromptFileReads >= 2, 4000);

    const toggle = document.getElementById("feature-prompt-context-introspect") as HTMLInputElement;
    expect(toggle.checked).toBe(false);

    toggle.checked = true;
    toggle.dispatchEvent(new Event("change", { bubbles: true }));

    await waitFor(() => window.localStorage.getItem("nextai.feature.prompt_context_introspect") === "true", 4000);
    await new Promise((resolve) => setTimeout(resolve, 50));

    toggle.checked = false;
    toggle.dispatchEvent(new Event("change", { bubbles: true }));
    await waitFor(() => window.localStorage.getItem("nextai.feature.prompt_context_introspect") === "false", 4000);
    await new Promise((resolve) => setTimeout(resolve, 50));

    toggle.checked = true;
    toggle.dispatchEvent(new Event("change", { bubbles: true }));
    await waitFor(() => window.localStorage.getItem("nextai.feature.prompt_context_introspect") === "true", 4000);
    await waitFor(() => systemLayersRequested > 0, 4000);
    expect(toggle.checked).toBe(true);
  });

  it("聊天区 Codex 提示词开关切换后，请求会携带对应 biz_params.prompt_mode", async () => {
    let processCalls = 0;
    const capturedModes: string[] = [];

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: [],
          provider_types: [],
          defaults: {},
          active_llm: {
            provider_id: "",
            model: "",
          },
        });
      }

      if (url.pathname === "/chats" && method === "GET") {
        return jsonResponse([]);
      }

      if (url.pathname === "/agent/process" && method === "POST") {
        processCalls += 1;
        const payload = JSON.parse(String(init?.body ?? "{}")) as {
          biz_params?: {
            prompt_mode?: string;
          };
        };
        capturedModes.push(payload.biz_params?.prompt_mode ?? "");
        const sse = [
          `data: ${JSON.stringify({ type: "assistant_delta", step: 1, delta: "ok" })}`,
          `data: ${JSON.stringify({ type: "completed", step: 1, reply: "ok" })}`,
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

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    const promptModeToggle = document.getElementById("chat-prompt-mode-toggle") as HTMLInputElement;
    const messageInput = document.getElementById("message-input") as HTMLTextAreaElement;
    expect(promptModeToggle.checked).toBe(false);

    messageInput.value = "first";
    messageInput.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));
    await waitFor(() => processCalls >= 1, 4000);
    expect(capturedModes[0]).toBe("default");

    promptModeToggle.checked = true;
    promptModeToggle.dispatchEvent(new Event("change", { bubbles: true }));

    messageInput.value = "second";
    messageInput.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));
    await waitFor(() => processCalls >= 2, 4000);
    expect(capturedModes[1]).toBe("codex");

    promptModeToggle.checked = false;
    promptModeToggle.dispatchEvent(new Event("change", { bubbles: true }));

    messageInput.value = "third";
    messageInput.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));
    await waitFor(() => processCalls >= 3, 4000);
    expect(capturedModes[2]).toBe("default");
  });

  it("会话间 prompt_mode 状态隔离：A=codex，B=default", async () => {
    let processCalls = 0;
    const captured: Array<{ sessionID: string; promptMode: string }> = [];
    const chats = [
      {
        id: "chat-codex",
        name: "Codex Chat",
        session_id: "session-codex",
        user_id: "demo-user",
        channel: "console",
        created_at: "2026-02-17T12:00:00Z",
        updated_at: "2026-02-17T12:00:20Z",
        meta: { prompt_mode: "codex" },
      },
      {
        id: "chat-default",
        name: "Default Chat",
        session_id: "session-default",
        user_id: "demo-user",
        channel: "console",
        created_at: "2026-02-17T12:00:01Z",
        updated_at: "2026-02-17T12:00:10Z",
        meta: { prompt_mode: "default" },
      },
    ];

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: [],
          provider_types: [],
          defaults: {},
          active_llm: {
            provider_id: "",
            model: "",
          },
        });
      }

      if (url.pathname === "/chats" && method === "GET") {
        return jsonResponse(chats);
      }

      if ((url.pathname === "/chats/chat-codex" || url.pathname === "/chats/chat-default") && method === "GET") {
        return jsonResponse({ messages: [] });
      }

      if (url.pathname === "/agent/process" && method === "POST") {
        processCalls += 1;
        const payload = JSON.parse(String(init?.body ?? "{}")) as {
          session_id?: string;
          biz_params?: {
            prompt_mode?: string;
          };
        };
        captured.push({
          sessionID: payload.session_id ?? "",
          promptMode: payload.biz_params?.prompt_mode ?? "",
        });
        const sse = [
          `data: ${JSON.stringify({ type: "assistant_delta", step: 1, delta: "ok" })}`,
          `data: ${JSON.stringify({ type: "completed", step: 1, reply: "ok" })}`,
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

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    const promptModeToggle = document.getElementById("chat-prompt-mode-toggle") as HTMLInputElement;
    const messageInput = document.getElementById("message-input") as HTMLTextAreaElement;

    await waitFor(() => document.querySelector<HTMLButtonElement>('#chat-list .chat-item-btn[data-chat-id="chat-codex"]') !== null, 4000);
    expect(promptModeToggle.checked).toBe(true);

    messageInput.value = "for codex";
    messageInput.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));
    await waitFor(() => processCalls >= 1, 4000);
    expect(captured[0]).toEqual({ sessionID: "session-codex", promptMode: "codex" });

    const defaultChatButton = document.querySelector<HTMLButtonElement>('#chat-list .chat-item-btn[data-chat-id="chat-default"]');
    expect(defaultChatButton).not.toBeNull();
    defaultChatButton?.click();
    await waitFor(() => promptModeToggle.checked === false, 4000);

    messageInput.value = "for default";
    messageInput.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));
    await waitFor(() => processCalls >= 2, 4000);
    expect(captured[1]).toEqual({ sessionID: "session-default", promptMode: "default" });
  });

  it("新会话默认 prompt_mode=default", async () => {
    let processCalls = 0;
    const capturedModes: string[] = [];

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: [],
          provider_types: [],
          defaults: {},
          active_llm: {
            provider_id: "",
            model: "",
          },
        });
      }

      if (url.pathname === "/chats" && method === "GET") {
        return jsonResponse([]);
      }

      if (url.pathname === "/agent/process" && method === "POST") {
        processCalls += 1;
        const payload = JSON.parse(String(init?.body ?? "{}")) as {
          biz_params?: {
            prompt_mode?: string;
          };
        };
        capturedModes.push(payload.biz_params?.prompt_mode ?? "");
        const sse = [
          `data: ${JSON.stringify({ type: "assistant_delta", step: 1, delta: "ok" })}`,
          `data: ${JSON.stringify({ type: "completed", step: 1, reply: "ok" })}`,
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

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    const promptModeToggle = document.getElementById("chat-prompt-mode-toggle") as HTMLInputElement;
    const newChatButton = document.getElementById("new-chat") as HTMLButtonElement;
    const messageInput = document.getElementById("message-input") as HTMLTextAreaElement;

    promptModeToggle.checked = true;
    promptModeToggle.dispatchEvent(new Event("change", { bubbles: true }));
    expect(promptModeToggle.checked).toBe(true);

    newChatButton.click();
    expect(promptModeToggle.checked).toBe(false);

    messageInput.value = "for new chat";
    messageInput.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));
    await waitFor(() => processCalls >= 1, 4000);
    expect(capturedModes[0]).toBe("default");
  });

  it("按输出顺序渲染：文本和工具调用交错时保持时间线顺序", async () => {
    let processCalled = false;
    let capturedCommand = "";

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: [],
          provider_types: [],
          defaults: {},
          active_llm: {
            provider_id: "",
            model: "",
          },
        });
      }

      if (url.pathname === "/chats" && method === "GET") {
        return jsonResponse([]);
      }

      if (url.pathname === "/agent/process" && method === "POST") {
        processCalled = true;
        const payload = JSON.parse(String(init?.body ?? "{}")) as {
          biz_params?: {
            tool?: {
              input?: { command?: string };
            };
          };
        };
        capturedCommand = payload.biz_params?.tool?.input?.command ?? "";
        const sse = [
          `data: ${JSON.stringify({ type: "assistant_delta", step: 1, delta: "先返回文本" })}`,
          `data: ${JSON.stringify({ type: "tool_call", step: 1, tool_call: { name: "shell", input: { command: capturedCommand } } })}`,
          `data: ${JSON.stringify({ type: "assistant_delta", step: 1, delta: "再返回文本" })}`,
          `data: ${JSON.stringify({ type: "completed", step: 1, reply: "先返回文本再返回文本" })}`,
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

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    const messageInput = document.getElementById("message-input") as HTMLTextAreaElement;
    messageInput.value = "/shell printf hello";
    messageInput.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));

    await waitFor(() => processCalled, 4000);
    expect(capturedCommand).toBe("printf hello");

    await waitFor(() => {
      const assistant = document.querySelector<HTMLLIElement>("#message-list .message.assistant:last-child");
      if (!assistant) {
        return false;
      }
      const childClassList = Array.from(assistant.children).map((item) => item.className);
      const summary = assistant.querySelector<HTMLElement>(".tool-call-summary")?.textContent ?? "";
      const text = assistant.textContent ?? "";
      return (
        childClassList.join("|") === "message-text|tool-call-list|message-text" &&
        summary.includes("bash printf hello") &&
        text.includes("先返回文本") &&
        text.includes("再返回文本")
      );
    }, 4000);
  });

  it("view 工具调用文案展示文件路径", async () => {
    let processCalled = false;
    const viewedPath = "/mnt/Files/CodeXR/AGENTS.md";

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: [],
          provider_types: [],
          defaults: {},
          active_llm: {
            provider_id: "",
            model: "",
          },
        });
      }

      if (url.pathname === "/chats" && method === "GET") {
        if (!processCalled) {
          return jsonResponse([]);
        }
        return jsonResponse([
          {
            id: "chat-view-1",
            name: "view",
            session_id: "session-view",
            user_id: "user-view",
            channel: "console",
            created_at: "2026-02-17T12:00:00Z",
            updated_at: "2026-02-17T12:00:10Z",
            meta: {},
          },
        ]);
      }

      if (url.pathname === "/agent/process" && method === "POST") {
        processCalled = true;
        const sse = [
          `data: ${JSON.stringify({ type: "tool_call", step: 1, tool_call: { name: "view", input: { items: [{ path: viewedPath, start: 1, end: 20 }] } } })}`,
          `data: ${JSON.stringify({ type: "assistant_delta", step: 1, delta: "已查看文件" })}`,
          `data: ${JSON.stringify({ type: "completed", step: 1, reply: "已查看文件" })}`,
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

      if (url.pathname === "/chats/chat-view-1" && method === "GET") {
        const rawToolCall = JSON.stringify({
          type: "tool_call",
          step: 1,
          tool_call: {
            name: "view",
            input: {
              items: [{ path: viewedPath, start: 1, end: 20 }],
            },
          },
        });
        return jsonResponse({
          messages: [
            {
              id: "msg-user",
              role: "user",
              type: "message",
              content: [{ type: "text", text: "查看文件" }],
            },
            {
              id: "msg-assistant",
              role: "assistant",
              type: "message",
              content: [{ type: "text", text: "已查看文件" }],
              metadata: {
                tool_call_notices: [{ raw: rawToolCall }],
                tool_order: 1,
                text_order: 2,
              },
            },
          ],
        });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    const messageInput = document.getElementById("message-input") as HTMLTextAreaElement;
    messageInput.value = "看一下这个文件";
    messageInput.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));

    await waitFor(() => processCalled, 4000);

    await waitFor(() => {
      const assistant = document.querySelector<HTMLLIElement>("#message-list .message.assistant:last-child");
      if (!assistant) {
        return false;
      }
      const summary = assistant.querySelector<HTMLElement>(".tool-call-summary")?.textContent ?? "";
      const raw = assistant.querySelector<HTMLElement>(".tool-call-raw")?.textContent ?? "";
      return summary.includes(`查看（${viewedPath}）`) && raw.includes(`\"path\":\"${viewedPath}\"`);
    }, 4000);

    const chatItemButton = document.querySelector<HTMLButtonElement>("#chat-list .chat-item-btn");
    chatItemButton?.click();

    await waitFor(() => {
      const assistant = document.querySelector<HTMLLIElement>("#message-list .message.assistant:last-child");
      if (!assistant) {
        return false;
      }
      const summary = assistant.querySelector<HTMLElement>(".tool-call-summary")?.textContent ?? "";
      return summary.includes(`查看（${viewedPath}）`);
    }, 4000);
  });

  it("edit 工具调用文案展示文件路径", async () => {
    let processCalled = false;
    const editedPath = "/mnt/Files/CodeXR/apps/web/src/main.ts";

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: [],
          provider_types: [],
          defaults: {},
          active_llm: {
            provider_id: "",
            model: "",
          },
        });
      }

      if (url.pathname === "/chats" && method === "GET") {
        if (!processCalled) {
          return jsonResponse([]);
        }
        return jsonResponse([
          {
            id: "chat-edit-1",
            name: "edit",
            session_id: "session-edit",
            user_id: "user-edit",
            channel: "console",
            created_at: "2026-02-17T12:10:00Z",
            updated_at: "2026-02-17T12:10:10Z",
            meta: {},
          },
        ]);
      }

      if (url.pathname === "/agent/process" && method === "POST") {
        processCalled = true;
        const sse = [
          `data: ${JSON.stringify({ type: "tool_call", step: 1, tool_call: { name: "edit", input: { items: [{ path: editedPath, start: 1, end: 2, text: "new text" }] } } })}`,
          `data: ${JSON.stringify({ type: "assistant_delta", step: 1, delta: "已编辑文件" })}`,
          `data: ${JSON.stringify({ type: "completed", step: 1, reply: "已编辑文件" })}`,
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

      if (url.pathname === "/chats/chat-edit-1" && method === "GET") {
        const rawToolCall = JSON.stringify({
          type: "tool_call",
          step: 1,
          tool_call: {
            name: "edit",
            input: {
              items: [{ path: editedPath, start: 1, end: 2, text: "new text" }],
            },
          },
        });
        return jsonResponse({
          messages: [
            {
              id: "msg-user",
              role: "user",
              type: "message",
              content: [{ type: "text", text: "编辑文件" }],
            },
            {
              id: "msg-assistant",
              role: "assistant",
              type: "message",
              content: [{ type: "text", text: "已编辑文件" }],
              metadata: {
                tool_call_notices: [{ raw: rawToolCall }],
                tool_order: 1,
                text_order: 2,
              },
            },
          ],
        });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    const messageInput = document.getElementById("message-input") as HTMLTextAreaElement;
    messageInput.value = "改一下这个文件";
    messageInput.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));

    await waitFor(() => processCalled, 4000);

    await waitFor(() => {
      const assistant = document.querySelector<HTMLLIElement>("#message-list .message.assistant:last-child");
      if (!assistant) {
        return false;
      }
      const summary = assistant.querySelector<HTMLElement>(".tool-call-summary")?.textContent ?? "";
      return summary.includes(`编辑（${editedPath}）`);
    }, 4000);

    const chatItemButton = document.querySelector<HTMLButtonElement>("#chat-list .chat-item-btn");
    chatItemButton?.click();

    await waitFor(() => {
      const assistant = document.querySelector<HTMLLIElement>("#message-list .message.assistant:last-child");
      if (!assistant) {
        return false;
      }
      const summary = assistant.querySelector<HTMLElement>(".tool-call-summary")?.textContent ?? "";
      return summary.includes(`编辑（${editedPath}）`);
    }, 4000);
  });

  it("Cron text 模式任务创建后支持编辑和删除", async () => {
    type CronJobPayload = {
      id: string;
      name: string;
      enabled: boolean;
      schedule: { type: string; cron: string; timezone?: string };
      task_type: "text" | "workflow";
      text?: string;
      workflow?: {
        version: "v1";
        nodes: Array<Record<string, unknown>>;
        edges: Array<Record<string, unknown>>;
      };
      dispatch: {
        type?: string;
        channel?: string;
        target: { user_id: string; session_id: string };
        mode?: string;
        meta?: Record<string, unknown>;
      };
      runtime: {
        max_concurrency?: number;
        timeout_seconds?: number;
        misfire_grace_seconds?: number;
      };
      meta?: Record<string, unknown>;
    };

    let cronJobs: CronJobPayload[] = [];
    let chatsGetCount = 0;
    let createCalled = false;
    let updateCallCount = 0;
    let deleteCalled = false;
    const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true);

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: [],
          provider_types: [],
          defaults: {},
          active_llm: {
            provider_id: "",
            model: "",
          },
        });
      }

      if (url.pathname === "/chats" && method === "GET") {
        chatsGetCount += 1;
        return jsonResponse([]);
      }

      if (url.pathname === "/cron/jobs" && method === "GET") {
        return jsonResponse(cronJobs);
      }

      if (url.pathname === "/cron/jobs" && method === "POST") {
        createCalled = true;
        const payload = JSON.parse(String(init?.body ?? "{}")) as CronJobPayload;
        cronJobs = [payload];
        return jsonResponse(payload);
      }

      const stateMatch = url.pathname.match(/^\/cron\/jobs\/([^/]+)\/state$/);
      if (stateMatch && method === "GET") {
        return jsonResponse({
          next_run_at: "2026-02-17T13:40:00Z",
        });
      }

      const jobMatch = url.pathname.match(/^\/cron\/jobs\/([^/]+)$/);
      const runMatch = url.pathname.match(/^\/cron\/jobs\/([^/]+)\/run$/);
      if (runMatch && method === "POST") {
        return jsonResponse({ started: true });
      }
      if (jobMatch && method === "PUT") {
        updateCallCount += 1;
        const payload = JSON.parse(String(init?.body ?? "{}")) as CronJobPayload;
        cronJobs = cronJobs.map((item) => (item.id === payload.id ? payload : item));
        return jsonResponse(payload);
      }
      if (jobMatch && method === "DELETE") {
        deleteCalled = true;
        const jobID = decodeURIComponent(jobMatch[1] ?? "");
        const before = cronJobs.length;
        cronJobs = cronJobs.filter((item) => item.id !== jobID);
        return jsonResponse({ deleted: before !== cronJobs.length });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    const cronTabButton = document.querySelector<HTMLButtonElement>('button[data-tab="cron"]');
    cronTabButton?.click();

    await waitFor(() => document.getElementById("cron-create-open-btn") !== null, 4000);

    const openCreateButton = document.getElementById("cron-create-open-btn") as HTMLButtonElement;
    const cronWorkbench = document.getElementById("cron-workbench") as HTMLElement;
    const cronForm = document.getElementById("cron-create-form") as HTMLFormElement;
    const cronID = document.getElementById("cron-id") as HTMLInputElement;
    const cronName = document.getElementById("cron-name") as HTMLInputElement;
    const cronInterval = document.getElementById("cron-interval") as HTMLInputElement;
    const cronSessionID = document.getElementById("cron-session-id") as HTMLInputElement;
    const cronTaskType = document.getElementById("cron-task-type") as HTMLSelectElement;
    const cronText = document.getElementById("cron-text") as HTMLTextAreaElement;
    const cronSubmit = document.getElementById("cron-submit-btn") as HTMLButtonElement;
    const cronTaskTypeContainer = cronTaskType.closest<HTMLDivElement>(".custom-select-container");

    expect(cronWorkbench.dataset.cronView).toBe("jobs");
    expect(cronTaskTypeContainer).not.toBeNull();
    expect(cronTaskTypeContainer?.querySelector<HTMLInputElement>(".options-search-input")).toBeNull();
    openCreateButton.click();
    expect(cronWorkbench.dataset.cronView).toBe("editor");
    expect(openCreateButton.hidden).toBe(true);
    cronTaskType.value = "text";
    cronTaskType.dispatchEvent(new Event("change", { bubbles: true }));
    cronID.value = "job-demo";
    cronName.value = "初始任务";
    cronInterval.value = "60s";
    cronSessionID.value = "session-demo";
    cronText.value = "hello cron";
    cronForm.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));

    await waitFor(() => createCalled, 4000);
    await waitFor(() => document.querySelector<HTMLButtonElement>('button[data-cron-edit="job-demo"]') !== null, 4000);
    expect(cronJobs[0]?.task_type).toBe("text");
    expect(cronJobs[0]?.text).toBe("hello cron");
    expect(cronJobs[0]?.dispatch.channel).toBe("console");
    expect(cronJobs[0]?.dispatch.target.user_id).toBe("demo-user");
    const enabledToggle = document.querySelector<HTMLInputElement>('input[data-cron-toggle-enabled="job-demo"]');
    expect(enabledToggle).not.toBeNull();
    expect(enabledToggle?.checked).toBe(true);
    const updatesBeforeToggle = updateCallCount;
    enabledToggle?.click();
    await waitFor(() => updateCallCount > updatesBeforeToggle, 4000);
    expect(cronJobs[0]?.enabled).toBe(false);

    cronJobs = cronJobs.map((job) => ({
      ...job,
      dispatch: {
        ...job.dispatch,
        channel: "qq",
        target: {
          ...job.dispatch.target,
          user_id: "legacy-user",
        },
      },
    }));
    const refreshCronButton = document.getElementById("refresh-cron") as HTMLButtonElement;
    refreshCronButton.click();
    await waitFor(() => document.querySelector<HTMLButtonElement>('button[data-cron-edit="job-demo"]') !== null, 4000);

    const editButton = document.querySelector<HTMLButtonElement>('button[data-cron-edit="job-demo"]');
    editButton?.click();
    expect(cronID.readOnly).toBe(true);
    expect(cronSubmit.textContent ?? "").toContain("PUT /cron/jobs/{job_id}");
    const updatesBeforeEdit = updateCallCount;

    cronName.value = "已更新任务";
    cronForm.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));

    await waitFor(() => updateCallCount > updatesBeforeEdit, 4000);
    expect(cronJobs[0]?.name).toBe("已更新任务");
    expect(cronJobs[0]?.dispatch.channel).toBe("console");
    expect(cronJobs[0]?.dispatch.target.user_id).toBe("demo-user");

    const runButton = document.querySelector<HTMLButtonElement>('button[data-cron-run="job-demo"]');
    const chatsCountBeforeRun = chatsGetCount;
    runButton?.click();
    await waitFor(() => chatsGetCount > chatsCountBeforeRun, 4000);

    const deleteButton = document.querySelector<HTMLButtonElement>('button[data-cron-delete="job-demo"]');
    deleteButton?.click();

    await waitFor(() => deleteCalled, 4000);
    expect(confirmSpy).toHaveBeenCalled();
    await waitFor(() => document.querySelector<HTMLButtonElement>('button[data-cron-edit="job-demo"]') === null, 4000);
    confirmSpy.mockRestore();
  });

  it("Cron 默认 workflow 模式可添加节点连线并提交", async () => {
    type CronWorkflowNodePayload = {
      id: string;
      type: "start" | "text_event" | "delay" | "if_event";
      text?: string;
      delay_seconds?: number;
      if_condition?: string;
    };
    type CronWorkflowEdgePayload = {
      id: string;
      source: string;
      target: string;
    };
    type CronJobPayload = {
      id: string;
      name: string;
      enabled: boolean;
      schedule: { type: string; cron: string; timezone?: string };
      task_type: "text" | "workflow";
      text?: string;
      workflow?: {
        version: "v1";
        nodes: CronWorkflowNodePayload[];
        edges: CronWorkflowEdgePayload[];
      };
      dispatch: {
        type?: string;
        channel?: string;
        target: { user_id: string; session_id: string };
        mode?: string;
        meta?: Record<string, unknown>;
      };
      runtime: {
        max_concurrency?: number;
        timeout_seconds?: number;
        misfire_grace_seconds?: number;
      };
    };

    let createdPayload: CronJobPayload | null = null;
    let cronJobs: CronJobPayload[] = [];

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: [],
          provider_types: [],
          defaults: {},
          active_llm: {
            provider_id: "",
            model: "",
          },
        });
      }

      if (url.pathname === "/chats" && method === "GET") {
        return jsonResponse([]);
      }

      if (url.pathname === "/cron/jobs" && method === "GET") {
        return jsonResponse(cronJobs);
      }

      if (url.pathname === "/cron/jobs" && method === "POST") {
        createdPayload = JSON.parse(String(init?.body ?? "{}")) as CronJobPayload;
        cronJobs = [createdPayload];
        return jsonResponse(createdPayload);
      }

      const stateMatch = url.pathname.match(/^\/cron\/jobs\/([^/]+)\/state$/);
      if (stateMatch && method === "GET") {
        return jsonResponse({
          next_run_at: "2026-02-17T13:40:00Z",
        });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    document.querySelector<HTMLButtonElement>('button[data-tab="cron"]')?.click();
    await waitFor(() => document.getElementById("cron-create-open-btn") !== null, 4000);

    const openCreateButton = document.getElementById("cron-create-open-btn") as HTMLButtonElement;
    const cronForm = document.getElementById("cron-create-form") as HTMLFormElement;
    const cronID = document.getElementById("cron-id") as HTMLInputElement;
    const cronName = document.getElementById("cron-name") as HTMLInputElement;
    const cronSessionID = document.getElementById("cron-session-id") as HTMLInputElement;
    const cronTaskType = document.getElementById("cron-task-type") as HTMLSelectElement;
    const cronWorkflowSection = document.getElementById("cron-workflow-section") as HTMLElement;
    const cronWorkflowFullscreenButton = document.getElementById("cron-workflow-fullscreen-btn") as HTMLButtonElement;
    const workflowNodesLayer = document.getElementById("cron-workflow-nodes") as HTMLElement;

    openCreateButton.click();
    expect(cronTaskType.value).toBe("workflow");
    expect(cronWorkflowSection.classList.contains("is-pseudo-fullscreen")).toBe(false);
    expect(cronWorkflowFullscreenButton.textContent ?? "").toContain("全屏");
    cronWorkflowFullscreenButton.click();
    expect(cronWorkflowSection.classList.contains("is-pseudo-fullscreen")).toBe(true);
    expect(cronWorkflowFullscreenButton.textContent ?? "").toContain("退出全屏");
    cronWorkflowFullscreenButton.click();
    expect(cronWorkflowSection.classList.contains("is-pseudo-fullscreen")).toBe(false);

    cronID.value = "job-workflow-create";
    cronName.value = "workflow-create";
    cronSessionID.value = "session-workflow-create";

    const openNodeEditorFromContextMenu = async (nodeID: string): Promise<void> => {
      const card = document.querySelector<HTMLElement>(`[data-cron-node-id="${nodeID}"]`);
      expect(card).not.toBeNull();
      card?.dispatchEvent(new MouseEvent("contextmenu", {
        bubbles: true,
        cancelable: true,
        button: 2,
        clientX: 120,
        clientY: 120,
      }));
      await waitFor(
        () => document.querySelector<HTMLButtonElement>(".cron-node-context-menu button[data-cron-node-menu-action='edit']") !== null,
        4000,
      );
      const editButton = document.querySelector<HTMLButtonElement>(
        ".cron-node-context-menu button[data-cron-node-menu-action='edit']",
      );
      expect(editButton).not.toBeNull();
      editButton?.click();
    };

    await openNodeEditorFromContextMenu("node-1");
    const node1TextInput = document.querySelector<HTMLTextAreaElement>("#cron-workflow-node-editor textarea");
    expect(node1TextInput).not.toBeNull();
    if (node1TextInput) {
      node1TextInput.value = "first message";
      node1TextInput.dispatchEvent(new Event("input", { bubbles: true }));
    }

    const addNodeFromCanvasContextMenu = async (
      action: "add-text" | "add-if" | "add-delay",
      clientX: number,
      clientY: number,
    ): Promise<void> => {
      workflowNodesLayer.dispatchEvent(new MouseEvent("contextmenu", {
        bubbles: true,
        cancelable: true,
        button: 2,
        clientX,
        clientY,
      }));
      await waitFor(() => {
        const menu = document.querySelector<HTMLElement>(".cron-node-context-menu");
        const actionButton = document.querySelector<HTMLButtonElement>(
          `.cron-node-context-menu button[data-cron-node-menu-action='${action}']`,
        );
        return Boolean(menu && !menu.classList.contains("is-hidden") && actionButton && !actionButton.hidden);
      }, 4000);
      const addButton = document.querySelector<HTMLButtonElement>(
        `.cron-node-context-menu button[data-cron-node-menu-action='${action}']`,
      );
      expect(addButton).not.toBeNull();
      addButton?.click();
    };

    await addNodeFromCanvasContextMenu("add-if", 560, 300);
    await waitFor(() => document.querySelector<HTMLElement>('[data-cron-node-id="node-2"]') !== null, 4000);

    const node2Card = document.querySelector<HTMLElement>('[data-cron-node-id="node-2"]');
    node2Card?.dispatchEvent(new MouseEvent("contextmenu", {
      bubbles: true,
      cancelable: true,
      button: 2,
      clientX: 132,
      clientY: 132,
    }));
    await waitFor(
      () => document.querySelector<HTMLButtonElement>(".cron-node-context-menu button[data-cron-node-menu-action='delete']") !== null,
      4000,
    );
    const node2DeleteMenuButton = document.querySelector<HTMLButtonElement>(
      ".cron-node-context-menu button[data-cron-node-menu-action='delete']",
    );
    expect(node2DeleteMenuButton?.disabled).toBe(false);
    const node2EditMenuButton = document.querySelector<HTMLButtonElement>(
      ".cron-node-context-menu button[data-cron-node-menu-action='edit']",
    );
    node2EditMenuButton?.click();
    const node2IfInput = document.querySelector<HTMLInputElement>("#cron-workflow-node-editor input[type='text'][placeholder='channel == console']");
    expect(node2IfInput).not.toBeNull();
    if (node2IfInput) {
      node2IfInput.value = "channel == console";
      node2IfInput.dispatchEvent(new Event("input", { bubbles: true }));
      node2IfInput.blur();
    }

    await addNodeFromCanvasContextMenu("add-delay", 720, 420);
    await waitFor(() => document.querySelector<HTMLElement>('[data-cron-node-id="node-3"]') !== null, 4000);

    const node1Out = document.querySelector<HTMLButtonElement>('[data-cron-node-out="node-1"]');
    node1Out?.click();
    const node2In = document.querySelector<HTMLButtonElement>('[data-cron-node-in="node-2"]');
    node2In?.click();

    const node2Out = document.querySelector<HTMLButtonElement>('[data-cron-node-out="node-2"]');
    node2Out?.click();
    const node3In = document.querySelector<HTMLButtonElement>('[data-cron-node-in="node-3"]');
    node3In?.click();

    await waitFor(
      () => document.querySelector<SVGPathElement>('[data-edge-id="edge-node-2-node-3"]') !== null,
      4000,
    );
    const node2ToNode3Edge = document.querySelector<SVGPathElement>('[data-edge-id="edge-node-2-node-3"]');
    node2ToNode3Edge?.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    window.dispatchEvent(new KeyboardEvent("keydown", { key: "Delete", bubbles: true, cancelable: true }));
    await waitFor(
      () => document.querySelector<SVGPathElement>('[data-edge-id="edge-node-2-node-3"]') === null,
      4000,
    );

    const reconnectNode2Out = document.querySelector<HTMLButtonElement>('[data-cron-node-out="node-2"]');
    reconnectNode2Out?.click();
    const reconnectNode3In = document.querySelector<HTMLButtonElement>('[data-cron-node-in="node-3"]');
    reconnectNode3In?.click();
    await waitFor(
      () => document.querySelector<SVGPathElement>('[data-edge-id="edge-node-2-node-3"]') !== null,
      4000,
    );

    cronForm.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    await waitFor(() => createdPayload !== null, 4000);

    expect(createdPayload?.task_type).toBe("workflow");
    expect(createdPayload?.text).toBeUndefined();
    expect(createdPayload?.workflow?.version).toBe("v1");
    expect(createdPayload?.workflow?.nodes.some((item) => item.id === "node-2" && item.type === "if_event")).toBe(true);
    expect(createdPayload?.workflow?.nodes.some((item) => item.id === "node-2" && item.if_condition === "channel == console")).toBe(true);
    expect(createdPayload?.workflow?.nodes.some((item) => item.id === "node-3" && item.type === "delay")).toBe(true);
    expect(
      createdPayload?.workflow?.edges.some((item) => item.source === "node-1" && item.target === "node-2"),
    ).toBe(true);
    expect(
      createdPayload?.workflow?.edges.some((item) => item.source === "node-2" && item.target === "node-3"),
    ).toBe(true);
    expect(
      createdPayload?.workflow?.nodes.some((item) => item.id === "node-1" && item.type === "text_event" && item.text === "first message"),
    ).toBe(true);
  });

  it("编辑 workflow 任务时可回显节点与最近执行明细", async () => {
    type CronJobPayload = {
      id: string;
      name: string;
      enabled: boolean;
      schedule: { type: string; cron: string; timezone?: string };
      task_type: "text" | "workflow";
      workflow?: {
        version: "v1";
        nodes: Array<{
          id: string;
          type: "start" | "text_event" | "delay" | "if_event";
          x: number;
          y: number;
          text?: string;
          delay_seconds?: number;
          if_condition?: string;
        }>;
        edges: Array<{ id: string; source: string; target: string }>;
      };
      dispatch: {
        type?: string;
        channel?: string;
        target: { user_id: string; session_id: string };
      };
      runtime: {
        max_concurrency?: number;
        timeout_seconds?: number;
        misfire_grace_seconds?: number;
      };
    };

    const cronJobs: CronJobPayload[] = [
      {
        id: "job-workflow-edit",
        name: "workflow-edit",
        enabled: true,
        schedule: {
          type: "interval",
          cron: "60s",
        },
        task_type: "workflow",
        workflow: {
          version: "v1",
          nodes: [
            { id: "start", type: "start", x: 80, y: 80 },
            { id: "node-1", type: "text_event", x: 360, y: 80, text: "alpha" },
            { id: "node-2", type: "delay", x: 640, y: 80, delay_seconds: 5 },
          ],
          edges: [
            { id: "edge-start-node-1", source: "start", target: "node-1" },
            { id: "edge-node-1-node-2", source: "node-1", target: "node-2" },
          ],
        },
        dispatch: {
          type: "channel",
          channel: "console",
          target: {
            user_id: "demo-user",
            session_id: "session-workflow-edit",
          },
        },
        runtime: {
          max_concurrency: 1,
          timeout_seconds: 30,
          misfire_grace_seconds: 0,
        },
      },
    ];

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: [],
          provider_types: [],
          defaults: {},
          active_llm: {
            provider_id: "",
            model: "",
          },
        });
      }

      if (url.pathname === "/chats" && method === "GET") {
        return jsonResponse([]);
      }

      if (url.pathname === "/cron/jobs" && method === "GET") {
        return jsonResponse(cronJobs);
      }

      const stateMatch = url.pathname.match(/^\/cron\/jobs\/([^/]+)\/state$/);
      if (stateMatch && method === "GET") {
        return jsonResponse({
          next_run_at: "2026-02-17T14:00:00Z",
          last_execution: {
            run_id: "run-1",
            started_at: "2026-02-17T14:00:00Z",
            finished_at: "2026-02-17T14:00:08Z",
            had_failures: true,
            nodes: [
              {
                node_id: "node-1",
                node_type: "text_event",
                status: "failed",
                continue_on_error: true,
                started_at: "2026-02-17T14:00:01Z",
                finished_at: "2026-02-17T14:00:02Z",
                error: "dispatch failed",
              },
              {
                node_id: "node-2",
                node_type: "delay",
                status: "succeeded",
                continue_on_error: true,
                started_at: "2026-02-17T14:00:03Z",
                finished_at: "2026-02-17T14:00:08Z",
              },
            ],
          },
        });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    document.querySelector<HTMLButtonElement>('button[data-tab="cron"]')?.click();
    await waitFor(() => document.querySelector<HTMLButtonElement>('button[data-cron-edit="job-workflow-edit"]') !== null, 4000);

    document.querySelector<HTMLButtonElement>('button[data-cron-edit="job-workflow-edit"]')?.click();
    await waitFor(() => document.getElementById("cron-create-modal")?.classList.contains("is-hidden") === false, 4000);

    const cronWorkbench = document.getElementById("cron-workbench") as HTMLElement;
    expect(cronWorkbench.dataset.cronView).toBe("editor");
    const cronTaskType = document.getElementById("cron-task-type") as HTMLSelectElement;
    expect(cronTaskType.value).toBe("workflow");
    expect(document.querySelector<HTMLElement>('[data-cron-node-id="node-2"]')).not.toBeNull();
    expect(document.querySelector<HTMLElement>("#cron-workflow-nodes")?.textContent ?? "").toContain("alpha");

    const executionListText = document.getElementById("cron-workflow-execution-list")?.textContent ?? "";
    expect(executionListText).toContain("node-1");
    expect(executionListText).toContain("文本事件");
    expect(executionListText).toContain("失败");
    expect(executionListText).toContain("dispatch failed");
    expect(executionListText).toContain("node-2");
    expect(executionListText).toContain("延时");
    expect(executionListText).toContain("成功");
  });

  it("cron 面板可返回聊天页并恢复会话列表可见", async () => {
    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: [],
          provider_types: [],
          defaults: {},
          active_llm: {
            provider_id: "",
            model: "",
          },
        });
      }

      if (url.pathname === "/chats" && method === "GET") {
        if (url.searchParams.get("channel") === "qq") {
          return jsonResponse([]);
        }
        return jsonResponse([
          {
            id: "chat-return-1",
            name: "Return target",
            session_id: "session-return-1",
            user_id: "demo-user",
            channel: "console",
            created_at: "2026-02-17T12:00:00Z",
            updated_at: "2026-02-17T12:00:10Z",
            meta: {},
          },
        ]);
      }

      if (url.pathname === "/chats/chat-return-1" && method === "GET") {
        return jsonResponse({
          messages: [
            {
              id: "msg-return-1",
              role: "assistant",
              type: "message",
              content: [{ type: "text", text: "return ok" }],
            },
          ],
        });
      }

      if (url.pathname === "/cron/jobs" && method === "GET") {
        return jsonResponse([]);
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    await waitFor(() => document.querySelector<HTMLButtonElement>("#chat-list .chat-item-btn") !== null, 4000);

    const openCronButton = document.getElementById("chat-cron-toggle") as HTMLButtonElement | null;
    expect(openCronButton).not.toBeNull();
    openCronButton?.click();

    await waitFor(() => document.getElementById("panel-cron")?.classList.contains("is-active") === true, 4000);

    const backToChatButton = document.getElementById("cron-chat-toggle") as HTMLButtonElement | null;
    expect(backToChatButton).not.toBeNull();
    backToChatButton?.click();

    await waitFor(() => document.getElementById("panel-chat")?.classList.contains("is-active") === true, 4000);
    await waitFor(() => {
      const listText = document.querySelector<HTMLElement>("#chat-list")?.textContent ?? "";
      return listText.includes("Return target");
    }, 4000);
  });

  it("聊天页打开会话后应自动刷新后台新增消息", async () => {
    let chatsRequestCount = 0;

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: [],
          provider_types: [],
          defaults: {},
          active_llm: {
            provider_id: "",
            model: "",
          },
        });
      }

      if (url.pathname === "/chats" && method === "GET") {
        chatsRequestCount += 1;
        const updatedAt = chatsRequestCount >= 2 ? "2026-02-17T13:00:20Z" : "2026-02-17T13:00:10Z";
        return jsonResponse([
          {
            id: "chat-live-1",
            name: "live-chat",
            session_id: "session-live-1",
            user_id: "demo-user",
            channel: "console",
            created_at: "2026-02-17T13:00:00Z",
            updated_at: updatedAt,
            meta: {},
          },
        ]);
      }

      if (url.pathname === "/chats/chat-live-1" && method === "GET") {
        if (chatsRequestCount >= 2) {
          return jsonResponse({
            messages: [
              {
                id: "msg-live-user",
                role: "user",
                type: "message",
                content: [{ type: "text", text: "你好" }],
              },
              {
                id: "msg-live-assistant",
                role: "assistant",
                type: "message",
                content: [{ type: "text", text: "实时新内容" }],
              },
            ],
          });
        }
        return jsonResponse({
          messages: [
            {
              id: "msg-live-user",
              role: "user",
              type: "message",
              content: [{ type: "text", text: "你好" }],
            },
          ],
        });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    await waitFor(() => chatsRequestCount >= 2, 6000);
    await waitFor(() => {
      const assistant = document.querySelector<HTMLLIElement>("#message-list .message.assistant:last-child");
      return (assistant?.textContent ?? "").includes("实时新内容");
    }, 6000);
  });

  it("搜索会话时聚合 QQ 渠道历史，且 QQ 拉取不按 user_id 过滤", async () => {
    let consoleChatsRequested = false;
    let qqChatsRequested = false;

    window.localStorage.setItem(
      "nextai.web.chat.settings",
      JSON.stringify({
        apiBase: "http://127.0.0.1:8088",
        apiKey: "",
        userId: "demo-user",
        channel: "qq",
      }),
    );

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: [],
          provider_types: [],
          defaults: {},
          active_llm: {
            provider_id: "",
            model: "",
          },
        });
      }

      if (url.pathname === "/chats" && method === "GET") {
        const channel = url.searchParams.get("channel");
        if (channel === "console") {
          consoleChatsRequested = true;
          expect(url.searchParams.get("user_id")).toBe("demo-user");
          return jsonResponse([
            {
              id: "chat-console-1",
              name: "Console chat",
              session_id: "session-console-1",
              user_id: "demo-user",
              channel: "console",
              created_at: "2026-02-17T12:59:00Z",
              updated_at: "2026-02-17T12:59:10Z",
              meta: {},
            },
          ]);
        }
        if (channel === "qq") {
          qqChatsRequested = true;
          expect(url.searchParams.has("user_id")).toBe(false);
          return jsonResponse([
            {
              id: "chat-qq-1",
              name: "QQ inbound",
              session_id: "qq:c2c:u-c2c",
              user_id: "u-c2c",
              channel: "qq",
              created_at: "2026-02-17T13:00:00Z",
              updated_at: "2026-02-17T13:00:10Z",
              meta: {},
            },
          ]);
        }
        throw new Error(`unexpected /chats channel: ${channel}`);
      }

      if (url.pathname === "/chats/chat-console-1" && method === "GET") {
        return jsonResponse({
          messages: [
            {
              id: "msg-console-user",
              role: "user",
              type: "message",
              content: [{ type: "text", text: "hello console" }],
            },
          ],
        });
      }

      if (url.pathname === "/chats/chat-qq-1" && method === "GET") {
        return jsonResponse({
          messages: [
            {
              id: "msg-qq-user",
              role: "user",
              type: "message",
              content: [{ type: "text", text: "hello qq" }],
            },
          ],
        });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    await waitFor(() => consoleChatsRequested, 4000);
    await waitFor(() => qqChatsRequested, 4000);
    await waitFor(() => {
      const text = document.querySelector<HTMLElement>("#chat-list")?.textContent ?? "";
      return text.includes("QQ inbound");
    }, 4000);

    const searchToggleButton = document.getElementById("chat-search-toggle") as HTMLButtonElement | null;
    expect(searchToggleButton).not.toBeNull();
    searchToggleButton?.click();

    await waitFor(() => {
      const text = document.querySelector<HTMLElement>("#search-chat-results")?.textContent ?? "";
      return text.includes("qq:c2c:u-c2c");
    }, 4000);
  });

  it("会话列表支持删除，并在删除当前会话后自动切到下一会话", async () => {
    let deleteCalled = false;
    let chats = [
      {
        id: "chat-delete-1",
        name: "Delete target",
        session_id: "session-delete-1",
        user_id: "demo-user",
        channel: "console",
        created_at: "2026-02-17T13:00:00Z",
        updated_at: "2026-02-17T13:00:10Z",
        meta: {},
      },
      {
        id: "chat-delete-2",
        name: "Keep target",
        session_id: "session-delete-2",
        user_id: "demo-user",
        channel: "console",
        created_at: "2026-02-17T13:01:00Z",
        updated_at: "2026-02-17T13:01:10Z",
        meta: {},
      },
    ];
    const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true);

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: [],
          provider_types: [],
          defaults: {},
          active_llm: {
            provider_id: "",
            model: "",
          },
        });
      }

      if (url.pathname === "/chats" && method === "GET") {
        return jsonResponse(chats);
      }

      if (url.pathname === "/chats/chat-delete-1" && method === "GET") {
        return jsonResponse({
          messages: [
            {
              id: "msg-delete-1",
              role: "assistant",
              type: "message",
              content: [{ type: "text", text: "第一条会话历史" }],
            },
          ],
        });
      }

      if (url.pathname === "/chats/chat-delete-2" && method === "GET") {
        return jsonResponse({
          messages: [
            {
              id: "msg-delete-2",
              role: "assistant",
              type: "message",
              content: [{ type: "text", text: "第二条会话历史" }],
            },
          ],
        });
      }

      if (url.pathname === "/chats/chat-delete-1" && method === "DELETE") {
        deleteCalled = true;
        chats = chats.filter((chat) => chat.id !== "chat-delete-1");
        return jsonResponse({ deleted: true });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    await waitFor(() => document.querySelectorAll("#chat-list .chat-delete-btn").length === 2, 4000);

    const deleteListItem = Array.from(document.querySelectorAll<HTMLLIElement>("#chat-list .chat-list-item")).find((item) =>
      (item.textContent ?? "").includes("Delete target"),
    );
    const deleteButton = deleteListItem?.querySelector<HTMLButtonElement>(".chat-delete-btn") ?? null;
    expect(deleteButton).not.toBeNull();
    deleteButton?.click();

    await waitFor(() => deleteCalled, 4000);
    await waitFor(() => {
      const items = Array.from(document.querySelectorAll<HTMLButtonElement>("#chat-list .chat-item-btn"));
      return items.length === 1;
    }, 4000);

    expect(confirmSpy).toHaveBeenCalledWith("确认删除会话 session-delete-1？该操作不可恢复。");
    expect((document.getElementById("chat-title")?.textContent ?? "").includes("Keep target")).toBe(true);
    expect((document.getElementById("status-line")?.textContent ?? "").includes("已删除会话：session-delete-1")).toBe(true);

    confirmSpy.mockRestore();
  });

  it("搜索页支持过滤会话并点击进入会话详情", async () => {
    let openedChatID = "";

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: [],
          provider_types: [],
          defaults: {},
          active_llm: {
            provider_id: "",
            model: "",
          },
        });
      }

      if (url.pathname === "/chats" && method === "GET") {
        return jsonResponse([
          {
            id: "chat-search-1",
            name: "Alpha one",
            session_id: "session-alpha-1",
            user_id: "demo-user",
            channel: "console",
            created_at: "2026-02-17T14:00:00Z",
            updated_at: "2026-02-17T14:00:10Z",
            meta: {},
          },
          {
            id: "chat-search-2",
            name: "Beta target",
            session_id: "session-alpha-2",
            user_id: "demo-user",
            channel: "console",
            created_at: "2026-02-17T14:01:00Z",
            updated_at: "2026-02-17T14:01:10Z",
            meta: {
              source: "cron",
              cron_job_id: "job-demo",
            },
          },
        ]);
      }

      if (url.pathname === "/chats/chat-search-1" && method === "GET") {
        openedChatID = "chat-search-1";
        return jsonResponse({
          messages: [
            {
              id: "msg-search-1",
              role: "assistant",
              type: "message",
              content: [{ type: "text", text: "alpha history" }],
            },
          ],
        });
      }

      if (url.pathname === "/chats/chat-search-2" && method === "GET") {
        openedChatID = "chat-search-2";
        return jsonResponse({
          messages: [
            {
              id: "msg-search-2",
              role: "assistant",
              type: "message",
              content: [{ type: "text", text: "beta history" }],
            },
          ],
        });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    await waitFor(() => openedChatID !== "", 4000);

    const searchToggleButton = document.getElementById("chat-search-toggle") as HTMLButtonElement | null;
    expect(searchToggleButton).not.toBeNull();
    searchToggleButton?.click();

    const searchInput = document.getElementById("search-chat-input") as HTMLInputElement;
    searchInput.value = "job-demo";
    searchInput.dispatchEvent(new Event("input", { bubbles: true }));

    await waitFor(() => {
      const buttons = Array.from(document.querySelectorAll<HTMLButtonElement>("#search-chat-results .search-result-btn"));
      return buttons.length === 1 && (buttons[0].textContent ?? "").includes("session-alpha-2");
    }, 4000);

    searchInput.value = "session-alpha-1";
    searchInput.dispatchEvent(new Event("input", { bubbles: true }));

    await waitFor(() => {
      const buttons = Array.from(document.querySelectorAll<HTMLButtonElement>("#search-chat-results .search-result-btn"));
      return buttons.length === 1 && (buttons[0].textContent ?? "").includes("session-alpha-1");
    }, 4000);

    const resultButton = document.querySelector<HTMLButtonElement>("#search-chat-results .search-result-btn");
    expect(resultButton).not.toBeNull();
    resultButton?.click();

    await waitFor(() => openedChatID === "chat-search-1", 4000);
    await waitFor(() => document.getElementById("panel-chat")?.classList.contains("is-active") === true, 4000);
    expect(document.getElementById("chat-session")?.textContent ?? "").toContain("session-alpha-1");
  });

  it("QQ 渠道切到沙箱环境后保存配置会写入沙箱 api_base", async () => {
    let qqConfigLoaded = false;
    let qqConfigSaved = false;
    let workspaceFilesRequested = false;
    let savedAPIBase = "";

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: [],
          provider_types: [],
          defaults: {},
          active_llm: {
            provider_id: "",
            model: "",
          },
        });
      }

      if (url.pathname === "/chats" && method === "GET") {
        return jsonResponse([]);
      }

      if (url.pathname === "/config/channels/qq" && method === "GET") {
        qqConfigLoaded = true;
        return jsonResponse({
          enabled: true,
          app_id: "102857552",
          client_secret: "secret-xxx",
          bot_prefix: "",
          target_type: "c2c",
          target_id: "",
          api_base: "https://api.sgroup.qq.com",
          token_url: "https://bots.qq.com/app/getAppAccessToken",
          timeout_seconds: 8,
        });
      }

      if (url.pathname === "/workspace/files" && method === "GET") {
        workspaceFilesRequested = true;
        return jsonResponse([]);
      }

      if (url.pathname === "/config/channels/qq" && method === "PUT") {
        qqConfigSaved = true;
        const payload = JSON.parse(String(init?.body ?? "{}")) as { api_base?: string };
        savedAPIBase = payload.api_base ?? "";
        return jsonResponse(payload);
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    const settingsToggleButton = document.getElementById("settings-toggle") as HTMLButtonElement;
    expect(settingsToggleButton).not.toBeNull();
    settingsToggleButton?.click();

    const channelsSectionButton = document.querySelector<HTMLButtonElement>('button[data-settings-section="channels"]');
    expect(channelsSectionButton).not.toBeNull();
    channelsSectionButton?.click();

    await waitFor(() => qqConfigLoaded, 4000);
    expect(document.getElementById("channels-level1-view")?.hasAttribute("hidden")).toBe(false);
    expect(document.getElementById("channels-level2-view")?.hasAttribute("hidden")).toBe(true);

    const qqChannelCard = document.querySelector<HTMLButtonElement>('button[data-channel-action="open"][data-channel-id="qq"]');
    expect(qqChannelCard).not.toBeNull();
    qqChannelCard?.click();

    await waitFor(() => document.getElementById("channels-level2-view")?.hasAttribute("hidden") === false, 4000);

    const envSelect = document.getElementById("qq-channel-api-env") as HTMLSelectElement;
    const qqForm = document.getElementById("qq-channel-form") as HTMLFormElement;
    expect(envSelect.value).toBe("production");

    envSelect.value = "sandbox";
    envSelect.dispatchEvent(new Event("change", { bubbles: true }));
    qqForm.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));

    await waitFor(() => qqConfigSaved, 4000);
    expect(savedAPIBase).toBe("https://sandbox.api.sgroup.qq.com");
    await waitFor(() => document.getElementById("channels-level1-view")?.hasAttribute("hidden") === false, 4000);
    expect(document.getElementById("channels-level2-view")?.hasAttribute("hidden")).toBe(true);
    expect(workspaceFilesRequested).toBe(false);
  });

  it("工作区文本文件应按原文编辑并以 content 字段保存", async () => {
    const filePath = "docs/AI/AGENTS.md";
    const rawContent = [
      "# AI Tool Guide",
      "你可以通过 POST /agent/process 触发工具调用。",
      '命令示例: {"shell":[{"command":"pwd"}]}',
    ].join("\n");
    let workspaceFileLoaded = false;
    let workspaceFileSaved = false;
    let savedContent = "";

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: [],
          provider_types: [],
          defaults: {},
          active_llm: {
            provider_id: "",
            model: "",
          },
        });
      }

      if (url.pathname === "/chats" && method === "GET") {
        return jsonResponse([]);
      }

      if (url.pathname === "/workspace/files" && method === "GET") {
        return jsonResponse({
          files: [{ path: filePath, kind: "config", size: rawContent.length }],
        });
      }

      if (url.pathname === `/workspace/files/${encodeURIComponent(filePath)}` && method === "GET") {
        workspaceFileLoaded = true;
        return jsonResponse({ content: rawContent });
      }

      if (url.pathname === `/workspace/files/${encodeURIComponent(filePath)}` && method === "PUT") {
        workspaceFileSaved = true;
        const payload = JSON.parse(String(init?.body ?? "{}")) as { content?: string };
        savedContent = payload.content ?? "";
        return jsonResponse({ updated: true });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    const settingsToggleButton = document.getElementById("settings-toggle") as HTMLButtonElement | null;
    expect(settingsToggleButton).not.toBeNull();
    settingsToggleButton?.click();

    const workspaceSectionButton = document.querySelector<HTMLButtonElement>('button[data-settings-section="workspace"]');
    expect(workspaceSectionButton).not.toBeNull();
    workspaceSectionButton?.click();

    await waitFor(() => document.querySelector<HTMLButtonElement>(`button[data-workspace-open="${filePath}"]`) !== null, 4000);

    const openButton = document.querySelector<HTMLButtonElement>(`button[data-workspace-open="${filePath}"]`);
    openButton?.click();

    await waitFor(() => workspaceFileLoaded, 4000);
    await waitFor(() => {
      const input = document.getElementById("workspace-file-content") as HTMLTextAreaElement | null;
      return input?.value === rawContent;
    }, 4000);

    const editorInput = document.getElementById("workspace-file-content") as HTMLTextAreaElement;
    expect(editorInput.value).toBe(rawContent);
    expect(editorInput.value.includes("\\n")).toBe(false);

    const newContent = `${rawContent}\n新增一行`;
    editorInput.value = newContent;
    const editorForm = document.getElementById("workspace-editor-form") as HTMLFormElement;
    editorForm.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));

    await waitFor(() => workspaceFileSaved, 4000);
    expect(savedContent).toBe(newContent);
  });

  it("工作区应新增 codex 提示词卡片并支持文件夹层层展开", async () => {
    const codexFilePaths = [
      "prompts/codex/codex-rs/core/prompt.md",
      "prompts/codex/codex-rs/core/templates/collaboration_mode/default.md",
      "prompts/codex/user-codex/prompts/check-fix.md",
    ];
    const openFilePath = codexFilePaths[0];
    let codexFileLoaded = false;

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: [],
          provider_types: [],
          defaults: {},
          active_llm: {
            provider_id: "",
            model: "",
          },
        });
      }

      if (url.pathname === "/chats" && method === "GET") {
        return jsonResponse([]);
      }

      if (url.pathname === "/workspace/files" && method === "GET") {
        return jsonResponse({
          files: codexFilePaths.map((path) => ({ path, kind: "config", size: path.length })),
        });
      }

      if (url.pathname === `/workspace/files/${encodeURIComponent(openFilePath)}` && method === "GET") {
        codexFileLoaded = true;
        return jsonResponse({ content: "# codex prompt file" });
      }

      if (url.pathname.startsWith("/workspace/files/") && method === "GET") {
        return jsonResponse({ content: "" });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    const settingsToggleButton = document.getElementById("settings-toggle") as HTMLButtonElement | null;
    expect(settingsToggleButton).not.toBeNull();
    settingsToggleButton?.click();

    const workspaceSectionButton = document.querySelector<HTMLButtonElement>('button[data-settings-section="workspace"]');
    expect(workspaceSectionButton).not.toBeNull();
    workspaceSectionButton?.click();

    await waitFor(() => {
      const button = document.querySelector<HTMLButtonElement>('button[data-workspace-action="open-codex"]');
      return Boolean(button && (button.textContent ?? "").includes("3"));
    }, 4000);
    const openCodexButton = document.querySelector<HTMLButtonElement>('button[data-workspace-action="open-codex"]');
    expect(openCodexButton).not.toBeNull();
    expect(openCodexButton?.textContent ?? "").toContain("3");
    openCodexButton?.click();

    await waitFor(() => document.getElementById("workspace-level2-codex-view")?.hasAttribute("hidden") === false, 4000);
    await waitFor(() => document.querySelector('button[data-workspace-folder-toggle="codex-rs"]') !== null, 4000);

    const rootFolderToggle = document.querySelector<HTMLButtonElement>('button[data-workspace-folder-toggle="codex-rs"]');
    expect(rootFolderToggle).not.toBeNull();
    expect(rootFolderToggle?.getAttribute("aria-expanded")).toBe("true");

    const coreFolderToggle = document.querySelector<HTMLButtonElement>('button[data-workspace-folder-toggle="codex-rs/core"]');
    expect(coreFolderToggle).not.toBeNull();
    expect(coreFolderToggle?.getAttribute("aria-expanded")).toBe("false");
    coreFolderToggle?.click();

    await waitFor(
      () => document.querySelector<HTMLButtonElement>('button[data-workspace-folder-toggle="codex-rs/core/templates"]') !== null,
      4000,
    );
    const templatesFolderToggle = document.querySelector<HTMLButtonElement>('button[data-workspace-folder-toggle="codex-rs/core/templates"]');
    expect(templatesFolderToggle).not.toBeNull();
    expect(templatesFolderToggle?.getAttribute("aria-expanded")).toBe("false");

    const openFileButton = document.querySelector<HTMLButtonElement>(`button[data-workspace-open="${openFilePath}"]`);
    expect(openFileButton).not.toBeNull();
    openFileButton?.click();
    await waitFor(() => codexFileLoaded, 4000);
  });

  it("工作区卡片应支持启用和禁用", async () => {
    const configPath = "config/channels.json";
    const promptPath = "prompts/ai-tools.md";
    const codexPath = "prompts/codex/user-codex/prompts/check-fix.md";

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: [],
          provider_types: [],
          defaults: {},
          active_llm: {
            provider_id: "",
            model: "",
          },
        });
      }

      if (url.pathname === "/chats" && method === "GET") {
        return jsonResponse([]);
      }

      if (url.pathname === "/workspace/files" && method === "GET") {
        return jsonResponse({
          files: [
            { path: configPath, kind: "config", size: 128 },
            { path: promptPath, kind: "config", size: 256 },
            { path: codexPath, kind: "config", size: 512 },
          ],
        });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    const settingsToggleButton = document.getElementById("settings-toggle") as HTMLButtonElement | null;
    expect(settingsToggleButton).not.toBeNull();
    settingsToggleButton?.click();

    const workspaceSectionButton = document.querySelector<HTMLButtonElement>('button[data-settings-section="workspace"]');
    expect(workspaceSectionButton).not.toBeNull();
    workspaceSectionButton?.click();

    await waitFor(() => document.querySelector<HTMLButtonElement>('button[data-workspace-action="open-config"]') !== null, 4000);

    const disableButton = document.querySelector<HTMLButtonElement>('button[data-workspace-toggle-card="config"]');
    expect(disableButton).not.toBeNull();
    expect(disableButton?.textContent ?? "").toContain("禁用");
    disableButton?.click();

    await waitFor(() => {
      const openConfigButton = document.querySelector<HTMLButtonElement>('button[data-workspace-action="open-config"]');
      return openConfigButton?.disabled === true;
    }, 4000);
    await waitFor(() => (document.getElementById("status-line")?.textContent ?? "").includes("已禁用卡片"), 4000);

    const persistedAfterDisable = window.localStorage.getItem("nextai.web.chat.settings");
    expect(persistedAfterDisable).not.toBeNull();
    const parsedAfterDisable = JSON.parse(String(persistedAfterDisable)) as {
      workspaceCardEnabled?: { config?: boolean };
    };
    expect(parsedAfterDisable.workspaceCardEnabled?.config).toBe(false);

    const disabledOpenConfigButton = document.querySelector<HTMLButtonElement>('button[data-workspace-action="open-config"]');
    disabledOpenConfigButton?.click();
    expect(document.getElementById("workspace-level2-config-view")?.hasAttribute("hidden")).toBe(true);

    const enableButton = document.querySelector<HTMLButtonElement>('button[data-workspace-toggle-card="config"]');
    expect(enableButton).not.toBeNull();
    expect(enableButton?.textContent ?? "").toContain("启用");
    enableButton?.click();

    await waitFor(() => {
      const openConfigButton = document.querySelector<HTMLButtonElement>('button[data-workspace-action="open-config"]');
      return openConfigButton?.disabled === false;
    }, 4000);
    await waitFor(() => {
      const disableAgainButton = document.querySelector<HTMLButtonElement>('button[data-workspace-toggle-card="config"]');
      return (disableAgainButton?.textContent ?? "").includes("禁用");
    }, 4000);
    const persistedAfterEnable = window.localStorage.getItem("nextai.web.chat.settings");
    expect(persistedAfterEnable).not.toBeNull();
    const parsedAfterEnable = JSON.parse(String(persistedAfterEnable)) as {
      workspaceCardEnabled?: { config?: boolean };
    };
    expect(parsedAfterEnable.workspaceCardEnabled?.config).toBe(true);

    const enabledOpenConfigButton = document.querySelector<HTMLButtonElement>('button[data-workspace-action="open-config"]');
    enabledOpenConfigButton?.click();
    await waitFor(() => document.getElementById("workspace-level2-config-view")?.hasAttribute("hidden") === false, 4000);
  });

  it("keeps settings popover open when closing workspace editor modal", async () => {
    const filePath = "docs/AI/AGENTS.md";
    const rawContent = "# AI Tool Guide";
    let workspaceFileLoaded = false;

    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: [],
          provider_types: [],
          defaults: {},
          active_llm: {
            provider_id: "",
            model: "",
          },
        });
      }

      if (url.pathname === "/chats" && method === "GET") {
        return jsonResponse([]);
      }

      if (url.pathname === "/workspace/files" && method === "GET") {
        return jsonResponse({
          files: [{ path: filePath, kind: "config", size: rawContent.length }],
        });
      }

      if (url.pathname === `/workspace/files/${encodeURIComponent(filePath)}` && method === "GET") {
        workspaceFileLoaded = true;
        return jsonResponse({ content: rawContent });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    const settingsToggleButton = document.getElementById("settings-toggle") as HTMLButtonElement | null;
    expect(settingsToggleButton).not.toBeNull();
    settingsToggleButton?.click();

    const workspaceSectionButton = document.querySelector<HTMLButtonElement>('button[data-settings-section="workspace"]');
    expect(workspaceSectionButton).not.toBeNull();
    workspaceSectionButton?.click();

    await waitFor(() => document.querySelector<HTMLButtonElement>(`button[data-workspace-open="${filePath}"]`) !== null, 4000);
    const openButton = document.querySelector<HTMLButtonElement>(`button[data-workspace-open="${filePath}"]`);
    openButton?.click();

    await waitFor(() => workspaceFileLoaded, 4000);
    await waitFor(() => !((document.getElementById("workspace-editor-modal") as HTMLElement).classList.contains("is-hidden")), 4000);

    const settingsPopover = document.getElementById("settings-popover") as HTMLElement;
    const workspaceEditorModal = document.getElementById("workspace-editor-modal") as HTMLElement;
    const editorCloseButton = document.getElementById("workspace-editor-modal-close-btn") as HTMLButtonElement;
    expect(settingsPopover.classList.contains("is-hidden")).toBe(false);
    expect(workspaceEditorModal.classList.contains("is-hidden")).toBe(false);

    editorCloseButton.click();
    await waitFor(() => workspaceEditorModal.classList.contains("is-hidden"), 4000);
    expect(settingsPopover.classList.contains("is-hidden")).toBe(false);
  });

  it("disables delete action for protected default cron job but keeps enable toggle editable", async () => {
    type CronJobPayload = {
      id: string;
      name: string;
      enabled: boolean;
      schedule: { type: string; cron: string; timezone?: string };
      task_type: "text" | "workflow";
      text?: string;
      dispatch: {
        type?: string;
        channel?: string;
        target: { user_id: string; session_id: string };
        mode?: string;
        meta?: Record<string, unknown>;
      };
      runtime: {
        max_concurrency?: number;
        timeout_seconds?: number;
        misfire_grace_seconds?: number;
      };
      meta?: Record<string, unknown>;
    };

    let updateCallCount = 0;
    let deleteCalled = false;
    let cronJobs: CronJobPayload[] = [
      {
        id: "cron-default",
        name: "你好文本任务",
        enabled: false,
        schedule: { type: "interval", cron: "60s" },
        task_type: "text",
        text: "你好",
        dispatch: {
          type: "channel",
          channel: "console",
          target: {
            user_id: "demo-user",
            session_id: "session-default",
          },
          mode: "",
          meta: {},
        },
        runtime: {
          max_concurrency: 1,
          timeout_seconds: 30,
          misfire_grace_seconds: 0,
        },
        meta: {
          system_default: true,
        },
      },
    ];

    const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true);
    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const rawURL = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      const url = new URL(rawURL);
      const method = (init?.method ?? "GET").toUpperCase();

      if (url.pathname === "/models/catalog" && method === "GET") {
        return jsonResponse({
          providers: [],
          provider_types: [],
          defaults: {},
          active_llm: {
            provider_id: "",
            model: "",
          },
        });
      }

      if (url.pathname === "/chats" && method === "GET") {
        return jsonResponse([]);
      }

      if (url.pathname === "/cron/jobs" && method === "GET") {
        return jsonResponse(cronJobs);
      }

      const stateMatch = url.pathname.match(/^\/cron\/jobs\/([^/]+)\/state$/);
      if (stateMatch && method === "GET") {
        return jsonResponse({});
      }

      const updateMatch = url.pathname.match(/^\/cron\/jobs\/([^/]+)$/);
      if (updateMatch && method === "PUT") {
        updateCallCount += 1;
        const payload = JSON.parse(String(init?.body ?? "{}")) as CronJobPayload;
        cronJobs = cronJobs.map((job) => (job.id === payload.id ? payload : job));
        return jsonResponse(payload);
      }

      if (updateMatch && method === "DELETE") {
        deleteCalled = true;
        return jsonResponse({ deleted: true });
      }

      throw new Error(`unexpected request: ${method} ${url.pathname}`);
    }) as typeof globalThis.fetch;

    await import("../../src/main.ts");

    document.querySelector<HTMLButtonElement>('button[data-tab="cron"]')?.click();
    await waitFor(() => document.querySelector<HTMLButtonElement>('button[data-cron-delete="cron-default"]') !== null, 4000);

    const deleteButton = document.querySelector<HTMLButtonElement>('button[data-cron-delete="cron-default"]');
    expect(deleteButton).not.toBeNull();
    expect(deleteButton?.disabled).toBe(true);
    expect(deleteButton?.title).toContain("默认定时任务不可删除");
    deleteButton?.click();
    await new Promise((resolve) => setTimeout(resolve, 30));
    expect(confirmSpy).not.toHaveBeenCalled();
    expect(deleteCalled).toBe(false);

    const enabledToggle = document.querySelector<HTMLInputElement>('input[data-cron-toggle-enabled="cron-default"]');
    expect(enabledToggle).not.toBeNull();
    expect(enabledToggle?.checked).toBe(false);
    enabledToggle?.click();
    await waitFor(() => updateCallCount > 0, 4000);
    expect(cronJobs[0]?.enabled).toBe(true);

    confirmSpy.mockRestore();
  });
});
