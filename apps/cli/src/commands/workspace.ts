import { Command } from "commander";
import { ApiClient } from "../client/api-client.js";
import { readFile, writeFile } from "node:fs/promises";
import { printResult } from "../io/output.js";
import { t } from "../i18n.js";

export function registerWorkspaceCommand(program: Command, client: ApiClient): void {
  const ws = program.command("workspace").description(t("cli.command.workspace"));

  ws.command("ls").action(async () => {
    printResult(await client.workspaceLs());
  });

  ws.command("cat").argument("<path>").action(async (path: string) => {
    printResult(await client.workspaceCat(path));
  });

  ws
    .command("put")
    .requiredOption("--path <path>")
    .option("--body <json>")
    .option("--file <path>")
    .action(async (opts: { path: string; body?: string; file?: string }) => {
      let raw = opts.body;
      if (opts.file) {
        raw = await readFile(opts.file, "utf8");
      }
      if (raw === undefined) {
        throw new Error(t("output.workspace_put_missing_content"));
      }
      let payload: unknown;
      try {
        payload = JSON.parse(raw);
      } catch {
        throw new Error(t("output.workspace_put_invalid_json"));
      }
      printResult(await client.workspacePut(opts.path, payload));
    });

  ws.command("rm").argument("<path>").action(async (path: string) => {
    printResult(await client.workspaceRm(path));
  });

  ws.command("export").option("--out <path>", "workspace.json", "workspace.json").action(async (opts: { out: string }) => {
    try {
      const payload = await client.workspaceExport();
      await writeFile(opts.out, JSON.stringify(payload, null, 2), "utf8");
      printResult({ written: opts.out });
    } catch (err) {
      if (err instanceof Error) {
        throw new Error(t("output.workspace_export_failed", { message: err.message }));
      }
      throw err;
    }
  });

  ws.command("import").requiredOption("--file <path>").action(async (opts: { file: string }) => {
    const raw = await readFile(opts.file, "utf8");
    let parsed: unknown;
    try {
      parsed = JSON.parse(raw);
    } catch {
      throw new Error(t("output.workspace_import_invalid_json"));
    }

    let body: unknown = parsed;
    if (!parsed || typeof parsed !== "object" || !("mode" in (parsed as Record<string, unknown>))) {
      body = {
        mode: "replace",
        payload: parsed,
      };
    }

    printResult(await client.workspaceImport(body));
  });
}
