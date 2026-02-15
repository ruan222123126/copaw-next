import { Command } from "commander";
import { ApiClient } from "../client/api-client.js";

export function registerCronCommand(program: Command, client: ApiClient): void {
  const cron = program.command("cron").description("cron jobs");

  cron.command("list").action(async () => {
    console.log(JSON.stringify(await client.get("/cron/jobs"), null, 2));
  });

  cron.command("create").requiredOption("--json <json>").action(async (opts: { json: string }) => {
    console.log(JSON.stringify(await client.post("/cron/jobs", JSON.parse(opts.json)), null, 2));
  });

  cron.command("update").argument("<jobId>").requiredOption("--json <json>").action(async (jobId: string, opts: { json: string }) => {
    console.log(JSON.stringify(await client.put(`/cron/jobs/${encodeURIComponent(jobId)}`, JSON.parse(opts.json)), null, 2));
  });

  cron.command("delete").argument("<jobId>").action(async (jobId: string) => {
    console.log(JSON.stringify(await client.delete(`/cron/jobs/${encodeURIComponent(jobId)}`), null, 2));
  });

  cron.command("pause").argument("<jobId>").action(async (jobId: string) => {
    console.log(JSON.stringify(await client.post(`/cron/jobs/${encodeURIComponent(jobId)}/pause`), null, 2));
  });

  cron.command("resume").argument("<jobId>").action(async (jobId: string) => {
    console.log(JSON.stringify(await client.post(`/cron/jobs/${encodeURIComponent(jobId)}/resume`), null, 2));
  });

  cron.command("run").argument("<jobId>").action(async (jobId: string) => {
    console.log(JSON.stringify(await client.post(`/cron/jobs/${encodeURIComponent(jobId)}/run`), null, 2));
  });

  cron.command("state").argument("<jobId>").action(async (jobId: string) => {
    console.log(JSON.stringify(await client.get(`/cron/jobs/${encodeURIComponent(jobId)}/state`), null, 2));
  });
}
