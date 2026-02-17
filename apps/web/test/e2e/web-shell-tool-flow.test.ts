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

  it("Cron 任务创建后支持编辑和删除", async () => {
    type CronJobPayload = {
      id: string;
      name: string;
      enabled: boolean;
      schedule: { type: string; cron: string; timezone?: string };
      task_type: string;
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

    let cronJobs: CronJobPayload[] = [];
    let chatsGetCount = 0;
    let createCalled = false;
    let updateCalled = false;
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
        updateCalled = true;
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
    const cronForm = document.getElementById("cron-create-form") as HTMLFormElement;
    const cronID = document.getElementById("cron-id") as HTMLInputElement;
    const cronName = document.getElementById("cron-name") as HTMLInputElement;
    const cronInterval = document.getElementById("cron-interval") as HTMLInputElement;
    const cronSessionID = document.getElementById("cron-session-id") as HTMLInputElement;
    const cronText = document.getElementById("cron-text") as HTMLTextAreaElement;
    const cronSubmit = document.getElementById("cron-submit-btn") as HTMLButtonElement;

    openCreateButton.click();
    cronID.value = "job-demo";
    cronName.value = "初始任务";
    cronInterval.value = "60s";
    cronSessionID.value = "session-demo";
    cronText.value = "hello cron";
    cronForm.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));

    await waitFor(() => createCalled, 4000);
    await waitFor(() => document.querySelector<HTMLButtonElement>('button[data-cron-edit="job-demo"]') !== null, 4000);
    expect(cronJobs[0]?.dispatch.channel).toBe("console");
    expect(cronJobs[0]?.dispatch.target.user_id).toBe("demo-user");

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

    cronName.value = "已更新任务";
    cronForm.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));

    await waitFor(() => updateCalled, 4000);
    expect(cronJobs[0]?.name).toBe("已更新任务");
    expect(cronJobs[0]?.dispatch.channel).toBe("console");
    expect(cronJobs[0]?.dispatch.target.user_id).toBe("demo-user");

    const runButton = document.querySelector<HTMLButtonElement>('button[data-cron-run="job-demo"]');
    const chatsCountBeforeRun = chatsGetCount;
    runButton?.click();
    await waitFor(() => chatsGetCount > chatsCountBeforeRun, 4000);

    const searchTabButton = document.querySelector<HTMLButtonElement>('button[data-tab="search"]');
    const chatsCountBeforeSearchTab = chatsGetCount;
    searchTabButton?.click();
    await waitFor(() => chatsGetCount > chatsCountBeforeSearchTab, 4000);

    const deleteButton = document.querySelector<HTMLButtonElement>('button[data-cron-delete="job-demo"]');
    deleteButton?.click();

    await waitFor(() => deleteCalled, 4000);
    expect(confirmSpy).toHaveBeenCalled();
    await waitFor(() => document.querySelector<HTMLButtonElement>('button[data-cron-edit="job-demo"]') === null, 4000);
    confirmSpy.mockRestore();
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
      return text.includes("qq:c2c:u-c2c");
    }, 4000);

    const searchTabButton = document.querySelector<HTMLButtonElement>('button[data-tab="search"]');
    searchTabButton?.click();

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
        name: "待删除会话",
        session_id: "session-delete-1",
        user_id: "demo-user",
        channel: "console",
        created_at: "2026-02-17T13:00:00Z",
        updated_at: "2026-02-17T13:00:10Z",
        meta: {},
      },
      {
        id: "chat-delete-2",
        name: "保留会话",
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
      (item.textContent ?? "").includes("session-delete-1"),
    );
    const deleteButton = deleteListItem?.querySelector<HTMLButtonElement>(".chat-delete-btn") ?? null;
    expect(deleteButton).not.toBeNull();
    deleteButton?.click();

    await waitFor(() => deleteCalled, 4000);
    await waitFor(() => {
      const items = Array.from(document.querySelectorAll<HTMLButtonElement>("#chat-list .chat-item-btn"));
      return items.length === 1 && (items[0].textContent ?? "").includes("session-delete-2");
    }, 4000);

    expect(confirmSpy).toHaveBeenCalledWith("确认删除会话 session-delete-1？该操作不可恢复。");
    expect((document.getElementById("chat-title")?.textContent ?? "").includes("保留会话")).toBe(true);
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

    const searchTabButton = document.querySelector<HTMLButtonElement>('button[data-tab="search"]');
    expect(searchTabButton).not.toBeNull();
    searchTabButton?.click();

    const searchInput = document.getElementById("search-chat-input") as HTMLInputElement;
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

    const channelsTabButton = document.querySelector<HTMLButtonElement>('button[data-tab="channels"]');
    expect(channelsTabButton).not.toBeNull();
    channelsTabButton?.click();

    await waitFor(() => qqConfigLoaded, 4000);

    const envSelect = document.getElementById("qq-channel-api-env") as HTMLSelectElement;
    const qqForm = document.getElementById("qq-channel-form") as HTMLFormElement;
    expect(envSelect.value).toBe("production");

    envSelect.value = "sandbox";
    envSelect.dispatchEvent(new Event("change", { bubbles: true }));
    qqForm.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));

    await waitFor(() => qqConfigSaved, 4000);
    expect(savedAPIBase).toBe("https://sandbox.api.sgroup.qq.com");
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

    const workspaceTabButton = document.querySelector<HTMLButtonElement>('button[data-tab="workspace"]');
    expect(workspaceTabButton).not.toBeNull();
    workspaceTabButton?.click();

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
});
