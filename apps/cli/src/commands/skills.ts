import { Command } from "commander";
import { ApiClient } from "../client/api-client.js";

export function registerSkillsCommand(program: Command, client: ApiClient): void {
  const skills = program.command("skills").description("skills management");

  skills.command("list").action(async () => {
    console.log(JSON.stringify(await client.get("/skills"), null, 2));
  });

  skills.command("create").requiredOption("--name <name>").requiredOption("--content <content>").action(async (opts: { name: string; content: string }) => {
    console.log(JSON.stringify(await client.post("/skills", { name: opts.name, content: opts.content }), null, 2));
  });

  skills.command("enable").argument("<name>").action(async (name: string) => {
    console.log(JSON.stringify(await client.post(`/skills/${encodeURIComponent(name)}/enable`), null, 2));
  });

  skills.command("disable").argument("<name>").action(async (name: string) => {
    console.log(JSON.stringify(await client.post(`/skills/${encodeURIComponent(name)}/disable`), null, 2));
  });

  skills.command("delete").argument("<name>").action(async (name: string) => {
    console.log(JSON.stringify(await client.delete(`/skills/${encodeURIComponent(name)}`), null, 2));
  });
}
