import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawn } from "node:child_process";
import test from "node:test";
import { createServer } from "node:net";

const gatewayDir = new URL("../../apps/gateway", import.meta.url);

const liveAPIKey =
  process.env.NEXTAI_LIVE_OPENAI_API_KEY ||
  process.env.OPENAI_API_KEY ||
  "";
const liveBaseURL =
  process.env.NEXTAI_LIVE_OPENAI_BASE_URL ||
  process.env.OPENAI_BASE_URL ||
  "https://api.openai.com/v1";
const liveModel = process.env.NEXTAI_LIVE_OPENAI_MODEL || "gpt-4o-mini";

test(
  "nightly live provider chain: configure openai -> process chat",
  {
    skip: !liveAPIKey,
    timeout: 180_000,
  },
  async () => {
    const port = await getFreePort();
    const dataDir = await mkdtemp(join(tmpdir(), "nextai-live-"));
    const gatewayBin = join(dataDir, "gateway-live");
    const baseURL = `http://127.0.0.1:${port}`;

    await runCommand("go", ["build", "-o", gatewayBin, "./cmd/gateway"], {
      cwd: gatewayDir,
    });

    const proc = spawn(gatewayBin, [], {
      cwd: gatewayDir,
      env: {
        ...process.env,
        NEXTAI_HOST: "127.0.0.1",
        NEXTAI_PORT: String(port),
        NEXTAI_DATA_DIR: dataDir,
      },
      stdio: ["ignore", "pipe", "pipe"],
    });

    let logs = "";
    proc.stdout.on("data", (chunk) => {
      logs += chunk.toString();
    });
    proc.stderr.on("data", (chunk) => {
      logs += chunk.toString();
    });

    try {
      await waitForHealth(`${baseURL}/healthz`);

      await requestJSON(`${baseURL}/models/openai/config`, {
        method: "PUT",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          api_key: liveAPIKey,
          base_url: liveBaseURL,
        }),
      });

      await requestJSON(`${baseURL}/models/active`, {
        method: "PUT",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          provider_id: "openai",
          model: liveModel,
        }),
      });

      const response = await requestJSON(`${baseURL}/agent/process`, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          input: [
            {
              role: "user",
              type: "message",
              content: [{ type: "text", text: "Reply with one short sentence for live health check." }],
            },
          ],
          session_id: `nightly-live-${Date.now()}`,
          user_id: "nightly-live-bot",
          channel: "console",
          stream: false,
        }),
      });

      assert.equal(typeof response.reply, "string");
      assert.ok(response.reply.trim().length > 0, "provider reply should not be empty");
    } finally {
      proc.kill("SIGTERM");
      await onceProcessExit(proc, 5000);
      await rm(dataDir, { recursive: true, force: true });
    }

    if (proc.exitCode !== null && proc.exitCode !== 0) {
      throw new Error(`gateway exited unexpectedly (${proc.exitCode})\n${logs}`);
    }
  },
);

async function requestJSON(url, init = undefined) {
  const response = await fetch(url, init);
  const text = await response.text();
  const parsed = text ? JSON.parse(text) : {};
  if (!response.ok) {
    const code = parsed?.error?.code ? `${parsed.error.code}: ` : "";
    const message = parsed?.error?.message ?? response.statusText;
    throw new Error(`${code}${message}`.trim());
  }
  return parsed;
}

async function waitForHealth(url, timeoutMs = 30_000) {
  const start = Date.now();
  let lastError = null;
  while (Date.now() - start < timeoutMs) {
    try {
      const response = await fetch(url);
      if (response.ok) {
        return;
      }
      lastError = new Error(`health status: ${response.status}`);
    } catch (error) {
      lastError = error;
    }
    await sleep(300);
  }
  throw new Error(`gateway did not become healthy in ${timeoutMs}ms: ${String(lastError)}`);
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function onceProcessExit(proc, timeoutMs) {
  await waitForExit(proc, timeoutMs);
  if (proc.exitCode === null) {
    proc.kill("SIGKILL");
    await waitForExit(proc, timeoutMs);
  }
}

function waitForExit(proc, timeoutMs) {
  if (proc.exitCode !== null) {
    return Promise.resolve();
  }
  return new Promise((resolve) => {
    const timer = setTimeout(resolve, timeoutMs);
    proc.once("exit", () => {
      clearTimeout(timer);
      resolve();
    });
  });
}

function getFreePort() {
  return new Promise((resolve, reject) => {
    const server = createServer();
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      const address = server.address();
      if (!address || typeof address === "string") {
        server.close(() => reject(new Error("failed to allocate port")));
        return;
      }
      const port = address.port;
      server.close((err) => {
        if (err) {
          reject(err);
          return;
        }
        resolve(port);
      });
    });
  });
}

function runCommand(command, args, options = {}) {
  return new Promise((resolve, reject) => {
    const proc = spawn(command, args, {
      ...options,
      stdio: ["ignore", "pipe", "pipe"],
    });
    let output = "";
    proc.stdout.on("data", (chunk) => {
      output += chunk.toString();
    });
    proc.stderr.on("data", (chunk) => {
      output += chunk.toString();
    });
    proc.once("error", reject);
    proc.once("exit", (code) => {
      if (code === 0) {
        resolve();
        return;
      }
      reject(new Error(`${command} ${args.join(" ")} failed (${code})\n${output}`));
    });
  });
}
