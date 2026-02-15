import { Command } from "commander";
import { ApiClient } from "../client/api-client.js";
import { writeFile } from "node:fs/promises";

export function registerWorkspaceCommand(program: Command, client: ApiClient): void {
  const ws = program.command("workspace").description("workspace operations");

  ws.command("download").option("--out <path>", "workspace.zip").action(async (opts: { out: string }) => {
    const base = process.env.COPAW_API_BASE ?? "http://127.0.0.1:8088";
    const res = await fetch(`${base}/workspace/download`);
    if (!res.ok) {
      throw new Error(`download failed: ${res.status}`);
    }
    const buf = Buffer.from(await res.arrayBuffer());
    await writeFile(opts.out, buf);
    console.log(JSON.stringify({ written: opts.out }, null, 2));
  });

  ws.command("upload").requiredOption("--file <path>").action(async (opts: { file: string }) => {
    console.log(JSON.stringify(await client.uploadWorkspace(opts.file), null, 2));
  });
}
