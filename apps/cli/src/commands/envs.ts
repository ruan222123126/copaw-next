import { Command } from "commander";
import { ApiClient } from "../client/api-client.js";

export function registerEnvsCommand(program: Command, client: ApiClient): void {
  const env = program.command("env").description("environment variables");

  env.command("list").action(async () => {
    console.log(JSON.stringify(await client.get("/envs"), null, 2));
  });

  env.command("set").requiredOption("--json <json>").action(async (opts: { json: string }) => {
    console.log(JSON.stringify(await client.put("/envs", JSON.parse(opts.json)), null, 2));
  });

  env.command("delete").argument("<key>").action(async (key: string) => {
    console.log(JSON.stringify(await client.delete(`/envs/${encodeURIComponent(key)}`), null, 2));
  });
}
