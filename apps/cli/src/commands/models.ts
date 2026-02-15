import { Command } from "commander";
import { ApiClient } from "../client/api-client.js";

export function registerModelsCommand(program: Command, client: ApiClient): void {
  const models = program.command("models").description("models/providers");

  models.command("list").action(async () => {
    console.log(JSON.stringify(await client.get("/models"), null, 2));
  });

  models
    .command("config")
    .argument("<providerId>")
    .option("--api-key <apiKey>")
    .option("--base-url <baseUrl>")
    .action(async (providerId: string, opts: { apiKey?: string; baseUrl?: string }) => {
      console.log(
        JSON.stringify(
          await client.put(`/models/${encodeURIComponent(providerId)}/config`, {
            api_key: opts.apiKey,
            base_url: opts.baseUrl,
          }),
          null,
          2,
        ),
      );
    });

  models.command("active-get").action(async () => {
    console.log(JSON.stringify(await client.get("/models/active"), null, 2));
  });

  models
    .command("active-set")
    .requiredOption("--provider-id <providerId>")
    .requiredOption("--model <model>")
    .action(async (opts: { providerId: string; model: string }) => {
      console.log(JSON.stringify(await client.put("/models/active", { provider_id: opts.providerId, model: opts.model }), null, 2));
    });
}
