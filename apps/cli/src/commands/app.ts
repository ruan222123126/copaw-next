import { Command } from "commander";
import { ApiClient } from "../client/api-client.js";

export function registerAppCommand(program: Command, client: ApiClient): void {
  program
    .command("app")
    .description("gateway app commands")
    .command("start")
    .description("print startup hint")
    .action(async () => {
      const health = await client.get<{ ok: boolean }>("/healthz");
      console.log(JSON.stringify({ connected: health.ok }, null, 2));
    });
}
