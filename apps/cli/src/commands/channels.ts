import { Command } from "commander";
import { ApiClient } from "../client/api-client.js";

export function registerChannelsCommand(program: Command, client: ApiClient): void {
  const channels = program.command("channels").description("channel configs");

  channels.command("list").action(async () => {
    console.log(JSON.stringify(await client.get("/config/channels"), null, 2));
  });

  channels.command("types").action(async () => {
    console.log(JSON.stringify(await client.get("/config/channels/types"), null, 2));
  });

  channels.command("get").argument("<name>").action(async (name: string) => {
    console.log(JSON.stringify(await client.get(`/config/channels/${encodeURIComponent(name)}`), null, 2));
  });

  channels.command("set").argument("<name>").requiredOption("--json <json>").action(async (name: string, opts: { json: string }) => {
    console.log(JSON.stringify(await client.put(`/config/channels/${encodeURIComponent(name)}`, JSON.parse(opts.json)), null, 2));
  });
}
