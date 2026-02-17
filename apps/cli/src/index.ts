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
import { registerTUICommand } from "./commands/tui.js";
import { printError, setOutputJSONMode } from "./io/output.js";
import { initializeLocale, setLocale, t } from "./i18n.js";

const program = new Command();
const client = new ApiClient();
const argv = rewriteLegacyBodyFlag(process.argv.slice(2));
const localeEnv = process.env.NEXTAI_LOCALE;

initializeLocale(argv, localeEnv);

program.name("nextai").description(t("cli.program.description")).version("0.1.0");
program.option("--json", t("cli.option.json"));
program.option("--locale <locale>", t("cli.option.locale"));
program.hook("preAction", (thisCommand) => {
  setLocale((thisCommand.optsWithGlobals() as { locale?: string }).locale ?? localeEnv);
  const enabled = Boolean(thisCommand.optsWithGlobals().json);
  setOutputJSONMode(enabled);
});

registerAppCommand(program, client);
registerChatsCommand(program, client);
registerCronCommand(program, client);
registerModelsCommand(program, client);
registerEnvsCommand(program, client);
registerSkillsCommand(program, client);
registerWorkspaceCommand(program, client);
registerChannelsCommand(program, client);
registerTUICommand(program, client);

program.parseAsync(["node", "nextai", ...argv]).catch((err) => {
  printError(err);
  process.exit(1);
});

function rewriteLegacyBodyFlag(argv: string[]): string[] {
  const rewritten = [...argv];
  const commandIndex = rewritten.findIndex((token) => token === "cron" || token === "env" || token === "channels");
  if (commandIndex < 0 || commandIndex+1 >= rewritten.length) {
    return rewritten;
  }

  const commandKey = `${rewritten[commandIndex]} ${rewritten[commandIndex + 1]}`;
  const commandsWithLegacyJSON = new Set(["cron create", "cron update", "env set", "channels set"]);
  if (!commandsWithLegacyJSON.has(commandKey)) {
    return rewritten;
  }

  for (let i = commandIndex + 2; i < rewritten.length; i += 1) {
    const token = rewritten[i];
    if (token === "--json" && i + 1 < rewritten.length && looksLikeJSONValue(rewritten[i + 1])) {
      rewritten[i] = "--body";
      return rewritten;
    }
    if (token.startsWith("--json=")) {
      const value = token.slice("--json=".length);
      if (!looksLikeJSONValue(value)) {
        continue;
      }
      rewritten[i] = `--body=${value}`;
      return rewritten;
    }
  }
  return rewritten;
}

function looksLikeJSONValue(raw: string): boolean {
  const text = raw.trim();
  if (text.length < 2) {
    return false;
  }
  const first = text[0];
  const last = text[text.length - 1];
  return (first === "{" && last === "}") || (first === "[" && last === "]");
}
