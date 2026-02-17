import assert from "node:assert/strict";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawn } from "node:child_process";
import test from "node:test";
import { createServer } from "node:net";

const gatewayDir = new URL("../../apps/gateway", import.meta.url);
const DEBUG = process.env.DEBUG_SMOKE === "1";

test("gateway chat flow e2e: create -> stream send -> history", { timeout: 90_000 }, async () => {
  debug("allocate port");
  const port = await getFreePort();
  debug(`port=${port}`);
  const dataDir = await mkdtemp(join(tmpdir(), "nextai-smoke-"));
  const gatewayBin = join(dataDir, "gateway-smoke");
  const baseURL = `http://127.0.0.1:${port}`;

  debug("build gateway");
  await runCommand("go", ["build", "-o", gatewayBin, "./cmd/gateway"], {
    cwd: gatewayDir,
  });
  debug("build done");

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
    debug("wait for health");
    await waitForHealth(`${baseURL}/healthz`);
    debug("health ok");

    const sessionID = `session-${Date.now()}`;
    const userID = "smoke-user";
    const channel = "console";
    const inputText = "hello smoke";

    const createdChat = await requestJSON(`${baseURL}/chats`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        session_id: sessionID,
        user_id: userID,
        channel,
        name: "smoke-chat",
        meta: {},
      }),
    });

    assert.equal(createdChat.session_id, sessionID);
    assert.equal(createdChat.user_id, userID);
    assert.equal(createdChat.channel, channel);
    assert.ok(createdChat.id, "chat id should exist");

    debug("chat created");

    debug("request stream start");
    const streamResponse = await fetch(`${baseURL}/agent/process`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        input: [{ role: "user", type: "message", content: [{ type: "text", text: inputText }] }],
        session_id: sessionID,
        user_id: userID,
        channel,
        stream: true,
      }),
    });
    assert.equal(streamResponse.ok, true, "stream request should succeed");
    assert.ok(streamResponse.body, "stream response body should exist");

    debug("streaming");
    const streamedReply = await readSSEDelta(streamResponse.body);
    assert.match(streamedReply, /Echo:\s*hello smoke/);
    debug("stream done");

    const chats = await requestJSON(
      `${baseURL}/chats?${new URLSearchParams({ user_id: userID, channel }).toString()}`,
    );
    assert.ok(Array.isArray(chats), "chat list should be array");
    assert.ok(chats.some((chat) => chat.id === createdChat.id), "created chat should be listed");

    const history = await requestJSON(`${baseURL}/chats/${encodeURIComponent(createdChat.id)}`);
    assert.ok(Array.isArray(history.messages), "history messages should be array");
    assert.equal(history.messages.length, 2, "history should contain user + assistant message");
    assert.equal(history.messages[0].role, "user");
    assert.equal(history.messages[1].role, "assistant");
    assert.match(history.messages[1].content?.[0]?.text ?? "", /Echo:\s*hello smoke/);
    debug("history checked");
  } finally {
    debug("cleanup begin");
    proc.kill("SIGTERM");
    await onceProcessExit(proc, 4000);
    await rm(dataDir, { recursive: true, force: true });
    debug("cleanup done");
  }

  if (proc.exitCode !== null && proc.exitCode !== 0) {
    throw new Error(`gateway exited unexpectedly (${proc.exitCode})\n${logs}`);
  }
});

test("gateway SSE boundary e2e: delta stream ends with DONE", { timeout: 90_000 }, async () => {
  const port = await getFreePort();
  const dataDir = await mkdtemp(join(tmpdir(), "nextai-smoke-sse-"));
  const gatewayBin = join(dataDir, "gateway-smoke");
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
    const inputText = "this is a long e2e input to enforce multiple sse chunks in response";
    const response = await fetch(`${baseURL}/agent/process`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        input: [{ role: "user", type: "message", content: [{ type: "text", text: inputText }] }],
        session_id: `session-sse-${Date.now()}`,
        user_id: "u-sse",
        channel: "console",
        stream: true,
      }),
    });
    assert.equal(response.ok, true, "stream request should succeed");
    assert.ok(response.body, "stream response body should exist");

    const streamed = await readSSEStream(response.body);
    assert.equal(streamed.done, true, "SSE stream should end with [DONE]");
    assert.ok(streamed.chunks >= 1, `expected at least one SSE delta chunk, got chunks=${streamed.chunks}`);
    assert.match(streamed.output, /Echo:\s*this is a long e2e input/);
  } finally {
    proc.kill("SIGTERM");
    await onceProcessExit(proc, 4000);
    await rm(dataDir, { recursive: true, force: true });
  }

  if (proc.exitCode !== null && proc.exitCode !== 0) {
    throw new Error(`gateway exited unexpectedly (${proc.exitCode})\n${logs}`);
  }
});

test("gateway error code consistency e2e: invalid request and unsupported channel", { timeout: 90_000 }, async () => {
  const port = await getFreePort();
  const dataDir = await mkdtemp(join(tmpdir(), "nextai-smoke-errors-"));
  const gatewayBin = join(dataDir, "gateway-smoke");
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

    const invalidChat = await requestError(`${baseURL}/chats`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ name: "missing fields" }),
    });
    assert.equal(invalidChat.status, 400);
    assert.equal(invalidChat.code, "invalid_chat");

    const missingChat = await requestError(`${baseURL}/chats/not-exists`);
    assert.equal(missingChat.status, 404);
    assert.equal(missingChat.code, "not_found");

    const unsupportedChannel = await requestError(`${baseURL}/agent/process`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        input: [{ role: "user", type: "message", content: [{ type: "text", text: "hello" }] }],
        session_id: `session-error-${Date.now()}`,
        user_id: "u-error",
        channel: "sms",
        stream: false,
      }),
    });
    assert.equal(unsupportedChannel.status, 400);
    assert.equal(unsupportedChannel.code, "channel_not_supported");
  } finally {
    proc.kill("SIGTERM");
    await onceProcessExit(proc, 4000);
    await rm(dataDir, { recursive: true, force: true });
  }

  if (proc.exitCode !== null && proc.exitCode !== 0) {
    throw new Error(`gateway exited unexpectedly (${proc.exitCode})\n${logs}`);
  }
});

test("gateway cron concurrency e2e: pre-existing lease returns cron_busy", { timeout: 90_000 }, async () => {
  const port = await getFreePort();
  const dataDir = await mkdtemp(join(tmpdir(), "nextai-smoke-concurrency-"));
  const gatewayBin = join(dataDir, "gateway-smoke");
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
    const jobID = `job-busy-${Date.now()}`;
    await createCronJob(baseURL, {
      id: jobID,
      name: jobID,
      enabled: false,
      schedule: { type: "interval", cron: "60s" },
      task_type: "text",
      text: "hello",
      dispatch: { target: { user_id: "u1", session_id: "s1" } },
      runtime: { max_concurrency: 1, timeout_seconds: 30 },
    });

    const leaseDir = join(dataDir, "cron-leases", Buffer.from(jobID).toString("base64url"));
    await mkdir(leaseDir, { recursive: true });
    await writeFile(
      join(leaseDir, "slot-0.json"),
      JSON.stringify({
        lease_id: "pre-busy-lease",
        job_id: jobID,
        owner: "smoke-test",
        slot: 0,
        acquired_at: new Date().toISOString(),
        expires_at: new Date(Date.now() + 60_000).toISOString(),
      }),
      "utf8",
    );

    const busy = await requestError(`${baseURL}/cron/jobs/${encodeURIComponent(jobID)}/run`, {
      method: "POST",
    });
    assert.equal(busy.status, 409);
    assert.equal(busy.code, "cron_busy");

    const state = await requestJSON(`${baseURL}/cron/jobs/${encodeURIComponent(jobID)}/state`);
    assert.equal(state.last_status, "failed");
    assert.match(state.last_error ?? "", /max_concurrency/);
  } finally {
    proc.kill("SIGTERM");
    await onceProcessExit(proc, 4000);
    await rm(dataDir, { recursive: true, force: true });
  }

  if (proc.exitCode !== null && proc.exitCode !== 0) {
    throw new Error(`gateway exited unexpectedly (${proc.exitCode})\n${logs}`);
  }
});

test("gateway cron DST boundary e2e: timezone next_run_at is deterministic", { timeout: 90_000 }, async () => {
  const port = await getFreePort();
  const dataDir = await mkdtemp(join(tmpdir(), "nextai-smoke-dst-"));
  const gatewayBin = join(dataDir, "gateway-smoke");
  const baseURL = `http://127.0.0.1:${port}`;
  const timeZone = "America/New_York";

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
    const now = new Date();

    const springJobID = `dst-spring-${Date.now()}`;
    await createCronJob(baseURL, {
      id: springJobID,
      name: springJobID,
      enabled: true,
      schedule: { type: "cron", cron: "30 2 8 3 *", timezone: timeZone },
      task_type: "text",
      text: "dst spring",
      dispatch: { target: { user_id: "u1", session_id: "s1" } },
    });
    const springState = await requestJSON(`${baseURL}/cron/jobs/${encodeURIComponent(springJobID)}/state`);
    const expectedSpring = findNextWallClockInstant({
      from: now,
      timeZone,
      month: 3,
      day: 8,
      hour: 2,
      minute: 30,
    });
    assert.ok(expectedSpring, "expected spring DST instant should be found");
    assert.equal(new Date(springState.next_run_at).toISOString(), expectedSpring.toISOString());

    const fallJobID = `dst-fall-${Date.now()}`;
    await createCronJob(baseURL, {
      id: fallJobID,
      name: fallJobID,
      enabled: true,
      schedule: { type: "cron", cron: "30 1 1 11 *", timezone: timeZone },
      task_type: "text",
      text: "dst fall",
      dispatch: { target: { user_id: "u1", session_id: "s1" } },
    });
    const fallState = await requestJSON(`${baseURL}/cron/jobs/${encodeURIComponent(fallJobID)}/state`);
    const expectedFall = findNextWallClockInstant({
      from: now,
      timeZone,
      month: 11,
      day: 1,
      hour: 1,
      minute: 30,
    });
    assert.ok(expectedFall, "expected fall DST instant should be found");
    assert.equal(new Date(fallState.next_run_at).toISOString(), expectedFall.toISOString());
  } finally {
    proc.kill("SIGTERM");
    await onceProcessExit(proc, 4000);
    await rm(dataDir, { recursive: true, force: true });
  }

  if (proc.exitCode !== null && proc.exitCode !== 0) {
    throw new Error(`gateway exited unexpectedly (${proc.exitCode})\n${logs}`);
  }
});

async function createCronJob(baseURL, payload) {
  return requestJSON(`${baseURL}/cron/jobs`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(payload),
  });
}

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

async function requestError(url, init = undefined) {
  const response = await fetch(url, init);
  const text = await response.text();
  const parsed = text ? JSON.parse(text) : {};
  if (response.ok) {
    throw new Error(`expected error response but got status ${response.status}`);
  }
  return {
    status: response.status,
    code: parsed?.error?.code ?? "",
    message: parsed?.error?.message ?? "",
    details: parsed?.error?.details ?? null,
  };
}

async function readSSEDelta(stream) {
  const result = await readSSEStream(stream);
  assert.equal(result.done, true, "SSE stream should end with [DONE]");
  return result.output;
}

async function readSSEStream(stream) {
  const reader = stream.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  let output = "";
  let done = false;
  let chunks = 0;

  while (!done) {
    const chunk = await reader.read();
    if (chunk.done) {
      break;
    }
    buffer += decoder.decode(chunk.value, { stream: true }).replaceAll("\r", "");
    const parsed = parseSSEBuffer(buffer);
    buffer = parsed.rest;
    output += parsed.delta;
    chunks += parsed.chunks;
    done = parsed.done;
    if (done) {
      await reader.cancel();
      break;
    }
  }

  buffer += decoder.decode().replaceAll("\r", "");
  if (!done && buffer.trim() !== "") {
    const parsed = parseSSEBuffer(`${buffer}\n\n`);
    output += parsed.delta;
    chunks += parsed.chunks;
    done = parsed.done;
  }
  return { output, done, chunks };
}

function parseSSEBuffer(raw) {
  let buffer = raw;
  let done = false;
  let delta = "";
  let chunks = 0;

  while (!done) {
    const boundary = buffer.indexOf("\n\n");
    if (boundary < 0) {
      break;
    }
    const block = buffer.slice(0, boundary);
    buffer = buffer.slice(boundary + 2);

    const dataLines = block
      .split("\n")
      .filter((line) => line.startsWith("data:"))
      .map((line) => line.slice(5).trimStart());
    if (dataLines.length === 0) {
      continue;
    }

    const data = dataLines.join("\n");
    if (data === "[DONE]") {
      done = true;
      break;
    }
    const payload = JSON.parse(data);
    if (typeof payload.delta === "string") {
      delta += payload.delta;
      chunks += 1;
    }
  }

  return { done, delta, rest: buffer, chunks };
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
    await sleep(250);
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

function debug(message) {
  if (!DEBUG) {
    return;
  }
  console.error(`[smoke] ${message}`);
}

function findNextWallClockInstant({ from, timeZone, month, day, hour, minute, maxYears = 4 }) {
  const fromUTC = new Date(from);
  const startYear = Number(getZonedParts(fromUTC, timeZone).year);
  for (let year = startYear; year <= startYear + maxYears; year++) {
    const utcStart = Date.UTC(year, month - 1, day, 0, 0) - 18 * 60 * 60 * 1000;
    const utcEnd = Date.UTC(year, month - 1, day, 23, 59) + 18 * 60 * 60 * 1000;
    for (let ts = utcStart; ts <= utcEnd; ts += 60 * 1000) {
      const candidate = new Date(ts);
      if (candidate <= fromUTC) {
        continue;
      }
      const parts = getZonedParts(candidate, timeZone);
      if (
        Number(parts.year) === year &&
        Number(parts.month) === month &&
        Number(parts.day) === day &&
        Number(parts.hour) === hour &&
        Number(parts.minute) === minute
      ) {
        return candidate;
      }
    }
  }
  return null;
}

function getZonedParts(date, timeZone) {
  const formatter = new Intl.DateTimeFormat("en-CA", {
    timeZone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  });
  const out = {};
  for (const part of formatter.formatToParts(date)) {
    if (part.type === "literal") {
      continue;
    }
    out[part.type] = part.value;
  }
  return out;
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
