#!/usr/bin/env node
import fs from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { createInterface } from "node:readline/promises";
import dotenv from "dotenv";
import OpenAI from "openai";
import { chromium } from "playwright";
import { z } from "zod";

dotenv.config();

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const LOG_DIR = path.join(__dirname, "logs");
const SHOT_DIR = path.join(__dirname, "shots");

class ToolExecutionError extends Error {
  constructor(code, message, details = undefined) {
    super(message);
    this.name = "ToolExecutionError";
    this.code = code;
    this.details = details;
  }
}

function parseBoolean(value, defaultValue) {
  if (value == null || value === "") {
    return defaultValue;
  }
  const normalized = String(value).trim().toLowerCase();
  return ["1", "true", "yes", "on"].includes(normalized);
}

function parseInteger(value, defaultValue) {
  if (value == null || value === "") {
    return defaultValue;
  }
  const parsed = Number.parseInt(String(value), 10);
  return Number.isNaN(parsed) ? defaultValue : parsed;
}

function requireEnv(name) {
  const value = process.env[name];
  if (!value || !String(value).trim()) {
    throw new Error(`缺少必填环境变量: ${name}`);
  }
  return String(value).trim();
}

function getConfig() {
  const blockedHosts = (process.env.BLOCKED_HOSTS ?? "")
    .split(",")
    .map((item) => item.trim().toLowerCase())
    .filter(Boolean);

  return {
    modelApiKey: requireEnv("MODEL_API_KEY"),
    modelBaseUrl: requireEnv("MODEL_BASE_URL"),
    modelName: requireEnv("MODEL_NAME"),
    headless: parseBoolean(process.env.HEADLESS, false),
    maxSteps: parseInteger(process.env.MAX_STEPS, 20),
    toolTimeoutMs: parseInteger(process.env.TOOL_TIMEOUT_MS, 15000),
    modelTimeoutMs: parseInteger(process.env.MODEL_TIMEOUT_MS, 60000),
    sessionTimeoutMs: parseInteger(process.env.SESSION_TIMEOUT_MS, 300000),
    riskConfirmTimeoutMs: parseInteger(process.env.RISK_CONFIRM_TIMEOUT_MS, 30000),
    browserLocale: process.env.BROWSER_LOCALE?.trim() || "zh-CN",
    browserTimezone: process.env.BROWSER_TIMEZONE?.trim() || "Asia/Shanghai",
    blockedHosts,
  };
}

function nowCompact() {
  return new Date().toISOString().replace(/[-:.TZ]/g, "");
}

function randomSuffix() {
  return Math.random().toString(36).slice(2, 8);
}

function normalizeAssistantText(content) {
  if (typeof content === "string") {
    return content.trim();
  }
  if (!Array.isArray(content)) {
    return "";
  }
  return content
    .map((part) => {
      if (typeof part === "string") {
        return part;
      }
      if (part && typeof part === "object") {
        if (typeof part.text === "string") {
          return part.text;
        }
        if (typeof part.content === "string") {
          return part.content;
        }
      }
      return "";
    })
    .join("")
    .trim();
}

function ensureHostNotBlocked(urlString, blockedHosts) {
  let url;
  try {
    url = new URL(urlString);
  } catch (_err) {
    throw new ToolExecutionError("invalid_url", `无效 URL: ${urlString}`);
  }

  const host = url.hostname.toLowerCase();
  const protocol = url.protocol.toLowerCase();
  if (protocol !== "http:" && protocol !== "https:") {
    throw new ToolExecutionError("invalid_url_protocol", `仅支持 http/https，当前: ${protocol}`);
  }

  const matched = blockedHosts.some((blocked) => host === blocked || host.endsWith(`.${blocked}`));
  if (matched) {
    throw new ToolExecutionError("domain_blocked", `域名命中黑名单: ${host}`, {
      host,
      blocked_hosts: blockedHosts,
    });
  }
}

function looksHighRisk(name, args) {
  if (!["click", "type", "open_url"].includes(name)) {
    return false;
  }
  const highRiskPattern = /(submit|pay|delete|remove|confirm|purchase|checkout|购买|支付|删除|提交|确认)/i;
  return highRiskPattern.test(JSON.stringify(args));
}

async function askWithTimeout(rl, question, timeoutMs) {
  let timer;
  try {
    return await Promise.race([
      rl.question(question),
      new Promise((resolve) => {
        timer = setTimeout(() => resolve("__TIMEOUT__"), timeoutMs);
      }),
    ]);
  } finally {
    if (timer) {
      clearTimeout(timer);
    }
  }
}

function toolResultOk(data) {
  return { ok: true, data };
}

function toolResultError(code, message, details = undefined) {
  return {
    ok: false,
    error: { code, message, details },
  };
}

async function withTimeout(fn, timeoutMs, code, message) {
  let timer;
  try {
    return await Promise.race([
      fn(),
      new Promise((_, reject) => {
        timer = setTimeout(() => {
          reject(new ToolExecutionError(code, message));
        }, timeoutMs);
      }),
    ]);
  } finally {
    if (timer) {
      clearTimeout(timer);
    }
  }
}

const TOOL_SCHEMAS = {
  open_url: z
    .object({
      url: z.string().min(1),
    })
    .strict(),
  click: z
    .object({
      selector: z.string().min(1),
    })
    .strict(),
  type: z
    .object({
      selector: z.string().min(1),
      text: z.string(),
      clear: z.boolean().optional(),
    })
    .strict(),
  extract_text: z
    .object({
      selector: z.string().min(1),
    })
    .strict(),
  screenshot: z
    .object({
      path: z.string().min(1).optional(),
      fullPage: z.boolean().optional(),
    })
    .strict(),
  scroll: z
    .object({
      x: z.number().optional(),
      y: z.number().optional(),
    })
    .strict(),
};

const TOOLS = [
  {
    type: "function",
    function: {
      name: "open_url",
      description: "打开指定网页（受域名黑名单限制）",
      parameters: {
        type: "object",
        properties: {
          url: { type: "string", description: "要打开的 URL" },
        },
        required: ["url"],
        additionalProperties: false,
      },
    },
  },
  {
    type: "function",
    function: {
      name: "click",
      description: "点击一个页面元素",
      parameters: {
        type: "object",
        properties: {
          selector: { type: "string", description: "CSS 选择器" },
        },
        required: ["selector"],
        additionalProperties: false,
      },
    },
  },
  {
    type: "function",
    function: {
      name: "type",
      description: "在输入框输入文本",
      parameters: {
        type: "object",
        properties: {
          selector: { type: "string", description: "CSS 选择器" },
          text: { type: "string", description: "要输入的内容" },
          clear: { type: "boolean", description: "是否先清空输入框" },
        },
        required: ["selector", "text"],
        additionalProperties: false,
      },
    },
  },
  {
    type: "function",
    function: {
      name: "extract_text",
      description: "提取页面元素文本",
      parameters: {
        type: "object",
        properties: {
          selector: { type: "string", description: "CSS 选择器" },
        },
        required: ["selector"],
        additionalProperties: false,
      },
    },
  },
  {
    type: "function",
    function: {
      name: "screenshot",
      description: "对当前页面截图",
      parameters: {
        type: "object",
        properties: {
          path: { type: "string", description: "截图保存路径（可选）" },
          fullPage: { type: "boolean", description: "是否整页截图" },
        },
        additionalProperties: false,
      },
    },
  },
  {
    type: "function",
    function: {
      name: "scroll",
      description: "滚动页面",
      parameters: {
        type: "object",
        properties: {
          x: { type: "number", description: "水平滚动偏移" },
          y: { type: "number", description: "垂直滚动偏移" },
        },
        additionalProperties: false,
      },
    },
  },
];

async function main() {
  const config = getConfig();
  await fs.mkdir(LOG_DIR, { recursive: true });
  await fs.mkdir(SHOT_DIR, { recursive: true });

  const runId = `${nowCompact()}-${randomSuffix()}`;
  const logFile = path.join(LOG_DIR, `${runId}.jsonl`);
  const rl = createInterface({ input: process.stdin, output: process.stdout });

  const appendLog = async (payload) => {
    const line = JSON.stringify({ timestamp: new Date().toISOString(), run_id: runId, ...payload });
    await fs.appendFile(logFile, `${line}\n`, "utf8");
  };

  const args = process.argv.slice(2);
  const task = args.join(" ").trim() || (await rl.question("请输入任务描述: "));
  if (!task) {
    throw new Error("任务为空，已退出。示例: node agent.js 打开 example.com 并读取 h1");
  }

  const openai = new OpenAI({
    apiKey: config.modelApiKey,
    baseURL: config.modelBaseUrl,
  });

  const browser = await chromium.launch({ headless: config.headless });
  const context = await browser.newContext({
    locale: config.browserLocale,
    timezoneId: config.browserTimezone,
  });
  const page = await context.newPage();

  const sessionStartedAt = Date.now();

  const handlers = {
    open_url: async ({ url }) => {
      ensureHostNotBlocked(url, config.blockedHosts);
      await withTimeout(
        () => page.goto(url, { waitUntil: "domcontentloaded", timeout: config.toolTimeoutMs }),
        config.toolTimeoutMs,
        "navigation_timeout",
        "页面加载超时"
      );
      return {
        current_url: page.url(),
        title: await page.title(),
      };
    },
    click: async ({ selector }) => {
      await withTimeout(
        () => page.waitForSelector(selector, { state: "visible", timeout: config.toolTimeoutMs }),
        config.toolTimeoutMs,
        "selector_not_found",
        `未找到可点击元素: ${selector}`
      );
      await withTimeout(
        () => page.click(selector, { timeout: config.toolTimeoutMs }),
        config.toolTimeoutMs,
        "click_failed",
        `点击失败: ${selector}`
      );
      return { clicked: selector, current_url: page.url() };
    },
    type: async ({ selector, text, clear }) => {
      const locator = page.locator(selector);
      await withTimeout(
        () => locator.waitFor({ state: "visible", timeout: config.toolTimeoutMs }),
        config.toolTimeoutMs,
        "selector_not_found",
        `未找到输入框: ${selector}`
      );
      if (clear) {
        await withTimeout(
          () => locator.fill("", { timeout: config.toolTimeoutMs }),
          config.toolTimeoutMs,
          "input_failed",
          `清空输入框失败: ${selector}`
        );
      }
      await withTimeout(
        () => locator.fill(text, { timeout: config.toolTimeoutMs }),
        config.toolTimeoutMs,
        "input_failed",
        `输入失败: ${selector}`
      );
      return { typed_selector: selector, text_length: text.length };
    },
    extract_text: async ({ selector }) => {
      await withTimeout(
        () => page.waitForSelector(selector, { state: "attached", timeout: config.toolTimeoutMs }),
        config.toolTimeoutMs,
        "selector_not_found",
        `未找到元素: ${selector}`
      );
      const text = await withTimeout(
        () => page.$eval(selector, (el) => (el.innerText ?? el.textContent ?? "").trim()),
        config.toolTimeoutMs,
        "extract_failed",
        `读取文本失败: ${selector}`
      );
      return { selector, text };
    },
    screenshot: async ({ path: customPath, fullPage }) => {
      const targetPath = customPath
        ? path.isAbsolute(customPath)
          ? customPath
          : path.join(__dirname, customPath)
        : path.join(SHOT_DIR, `${runId}-manual-${Date.now()}.png`);
      await fs.mkdir(path.dirname(targetPath), { recursive: true });
      await withTimeout(
        () => page.screenshot({ path: targetPath, fullPage: fullPage ?? true }),
        config.toolTimeoutMs,
        "screenshot_failed",
        "截图失败"
      );
      return { screenshot_path: targetPath };
    },
    scroll: async ({ x, y }) => {
      const offsetX = x ?? 0;
      const offsetY = y ?? 600;
      const position = await withTimeout(
        () =>
          page.evaluate(
            ({ sx, sy }) => {
              window.scrollBy(sx, sy);
              return { x: window.scrollX, y: window.scrollY };
            },
            { sx: offsetX, sy: offsetY }
          ),
        config.toolTimeoutMs,
        "scroll_failed",
        "滚动失败"
      );
      return { x: position.x, y: position.y };
    },
  };

  async function maybeConfirmRisk(name, args, step) {
    if (!looksHighRisk(name, args)) {
      return "approved";
    }
    const answer = await askWithTimeout(
      rl,
      `\n[风险拦截] step=${step} tool=${name} args=${JSON.stringify(args)}\n确认执行请输入 y，其他任意输入拒绝: `,
      config.riskConfirmTimeoutMs
    );
    if (answer === "__TIMEOUT__") {
      return "timeout";
    }
    return String(answer).trim().toLowerCase() === "y" ? "approved" : "rejected";
  }

  async function executeToolCall(call, step) {
    const toolName = call?.function?.name || "";
    const rawArguments = call?.function?.arguments || "{}";

    await appendLog({ step, stage: "tool_call", tool_name: toolName, raw_arguments: rawArguments });

    const schema = TOOL_SCHEMAS[toolName];
    if (!schema) {
      return toolResultError("unknown_tool", `未知工具: ${toolName}`);
    }

    let parsedArgs;
    try {
      parsedArgs = rawArguments ? JSON.parse(rawArguments) : {};
    } catch (err) {
      return toolResultError("invalid_tool_arguments", `工具参数不是合法 JSON: ${toolName}`, {
        raw_arguments: rawArguments,
        parse_error: err instanceof Error ? err.message : String(err),
      });
    }

    const valid = schema.safeParse(parsedArgs);
    if (!valid.success) {
      return toolResultError("invalid_tool_arguments", `工具参数校验失败: ${toolName}`, {
        issues: valid.error.issues,
      });
    }

    const riskDecision = await maybeConfirmRisk(toolName, valid.data, step);
    if (riskDecision === "rejected") {
      return toolResultError("risk_action_rejected", `高风险操作已拒绝: ${toolName}`);
    }
    if (riskDecision === "timeout") {
      return toolResultError("risk_confirmation_timeout", `高风险操作确认超时: ${toolName}`);
    }

    try {
      const startedAt = Date.now();
      const data = await withTimeout(
        () => handlers[toolName](valid.data),
        config.toolTimeoutMs,
        "tool_timeout",
        `工具执行超时: ${toolName}`
      );
      const result = toolResultOk({ ...data, duration_ms: Date.now() - startedAt });
      await appendLog({ step, stage: "tool_result", tool_name: toolName, result });
      return result;
    } catch (err) {
      const screenshotPath = path.join(SHOT_DIR, `${runId}-step${step}-error.png`);
      try {
        await page.screenshot({ path: screenshotPath, fullPage: true });
      } catch (_screenshotErr) {
        // ignore screenshot errors while handling primary failure
      }

      const code = err instanceof ToolExecutionError ? err.code : "tool_execution_failed";
      const message = err instanceof Error ? err.message : String(err);
      const details = {
        ...(err instanceof ToolExecutionError && err.details ? { reason: err.details } : {}),
        screenshot_path: screenshotPath,
      };

      const result = toolResultError(code, message, details);
      await appendLog({ step, stage: "tool_result", tool_name: toolName, result });
      return result;
    }
  }

  const messages = [
    {
      role: "system",
      content:
        "你是浏览器自动化代理。只能通过工具执行动作，优先最少步骤完成任务。遇到工具失败时先自我修复一次，再决定是否结束。禁止调用未声明的工具。",
    },
    {
      role: "user",
      content: task,
    },
  ];

  let finalText = "";
  let finished = false;

  try {
    for (let step = 1; step <= config.maxSteps; step += 1) {
      if (Date.now() - sessionStartedAt > config.sessionTimeoutMs) {
        throw new Error(`会话超时（>${config.sessionTimeoutMs}ms）`);
      }

      const completion = await withTimeout(
        () =>
          openai.chat.completions.create({
            model: config.modelName,
            temperature: 0.2,
            tools: TOOLS,
            tool_choice: "auto",
            messages,
          }),
        config.modelTimeoutMs,
        "model_timeout",
        "模型响应超时"
      );

      const assistant = completion?.choices?.[0]?.message;
      if (!assistant) {
        throw new Error("模型未返回有效消息");
      }

      const assistantText = normalizeAssistantText(assistant.content);
      const toolCalls = Array.isArray(assistant.tool_calls) ? assistant.tool_calls : [];
      const normalizedToolCalls = toolCalls.map((call, index) => ({
        ...call,
        id: call.id || `call-${step}-${index + 1}`,
      }));

      const assistantMessageForHistory = {
        role: "assistant",
        content: assistantText || null,
      };
      if (normalizedToolCalls.length > 0) {
        assistantMessageForHistory.tool_calls = normalizedToolCalls;
      }
      messages.push(assistantMessageForHistory);

      await appendLog({
        step,
        stage: "assistant",
        assistant_text: assistantText,
        tool_call_count: normalizedToolCalls.length,
      });

      if (normalizedToolCalls.length === 0) {
        finalText = assistantText || "任务执行完成，但模型未返回文本结果。";
        finished = true;
        break;
      }

      for (const call of normalizedToolCalls) {
        const toolResult = await executeToolCall(call, step);
        messages.push({
          role: "tool",
          tool_call_id: call.id,
          content: JSON.stringify(toolResult),
        });
      }
    }
  } finally {
    await appendLog({
      stage: "session_end",
      final: finished,
      final_text: finalText,
      elapsed_ms: Date.now() - sessionStartedAt,
    });
    await context.close();
    await browser.close();
    rl.close();
  }

  if (!finished) {
    throw new Error(`达到最大步数 ${config.maxSteps}，任务未完成`);
  }

  console.log("\n=== 任务结果 ===");
  console.log(finalText);
  console.log(`\nrun_id: ${runId}`);
  console.log(`log: ${logFile}`);
  console.log(`shots: ${SHOT_DIR}`);
}

main().catch((err) => {
  const message = err instanceof Error ? err.message : String(err);
  console.error(`执行失败: ${message}`);
  process.exitCode = 1;
});
