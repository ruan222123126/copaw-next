import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { parse } from "yaml";

const specPath = new URL("../../packages/contracts/openapi/openapi.yaml", import.meta.url);

const requiredPaths = [
  "/version",
  "/healthz",
  "/chats",
  "/chats/{chat_id}",
  "/agent/process",
  "/channels/qq/inbound",
  "/channels/qq/state",
  "/cron/jobs",
  "/models",
  "/models/catalog",
  "/models/{provider_id}",
  "/envs",
  "/skills",
  "/workspace/files",
  "/workspace/files/{file_path}",
  "/workspace/export",
  "/workspace/import",
  "/config/channels",
];

test("openapi contains required paths", async () => {
  const raw = await readFile(specPath, "utf8");
  const spec = parse(raw);
  assert.ok(spec?.paths, "paths section should exist");
  for (const p of requiredPaths) {
    assert.ok(spec.paths[p], `missing path: ${p}`);
  }
});

test("openapi defines error schema", async () => {
  const raw = await readFile(specPath, "utf8");
  const spec = parse(raw);
  assert.equal(spec.openapi, "3.0.3");
  assert.ok(spec.components.schemas.ChatSpec);
  assert.equal(spec.components.securitySchemes.ApiKeyAuth.type, "apiKey");
});
