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
    window.localStorage.setItem("copaw-next.web.locale", "zh-CN");
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
            raw.includes('"type":"tool_call"') &&
            raw.includes('"name":"shell"') &&
            raw.includes('"command":"printf hello"') &&
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
      return firstClass === "tool-call-list" && summary.includes("bash printf hello") && raw.includes('"command":"printf hello"');
    }, 4000);
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
});
