#!/usr/bin/env node
import { Command } from "commander";
import { ApiClient } from "./client/api-client.js";
import { registerAppCommand } from "./commands/app.js";
import { registerChatsCommand } from "./commands/chats.js";
import { registerCronCommand } from "./commands/cron.js";
import { registerModelsCommand } from "./commands/models.js";
import { registerEnvsCommand } from "./commands/envs.js";
import { registerSkillsCommand } from "./commands/skills.js";
import { registerWorkspaceCommand } from "./commands/workspace.js";
import { registerChannelsCommand } from "./commands/channels.js";

const program = new Command();
const client = new ApiClient();

program.name("copaw").description("CoPaw Next CLI").version("0.1.0");

registerAppCommand(program, client);
registerChatsCommand(program, client);
registerCronCommand(program, client);
registerModelsCommand(program, client);
registerEnvsCommand(program, client);
registerSkillsCommand(program, client);
registerWorkspaceCommand(program, client);
registerChannelsCommand(program, client);

program.parseAsync(process.argv).catch((err) => {
  console.error(err instanceof Error ? err.message : String(err));
  process.exit(1);
});
