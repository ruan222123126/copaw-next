import type { CliMessageKey } from "../locales/zh-CN.js";

export type SlashCommandAction = "history" | "new_chat" | "refresh" | "settings" | "exit";

export interface SlashCommand {
  name: string;
  descriptionKey: CliMessageKey;
  action: SlashCommandAction;
}

export const slashCommands: SlashCommand[] = [
  { name: "/history", descriptionKey: "tui.command.history", action: "history" },
  { name: "/new", descriptionKey: "tui.command.new", action: "new_chat" },
  { name: "/refresh", descriptionKey: "tui.command.refresh", action: "refresh" },
  { name: "/settings", descriptionKey: "tui.command.settings", action: "settings" },
  { name: "/exit", descriptionKey: "tui.command.exit", action: "exit" },
];

export function isSlashDraft(raw: string): boolean {
  return raw.trimStart().startsWith("/");
}

export function filterSlashCommands(raw: string, commands: SlashCommand[] = slashCommands): SlashCommand[] {
  if (!isSlashDraft(raw)) {
    return [];
  }
  const query = raw.trim().toLowerCase();
  return commands.filter((item) => item.name.startsWith(query));
}

export function resolveSlashCommand(
  raw: string,
  selectionIndex = 0,
  commands: SlashCommand[] = slashCommands,
): SlashCommand | null {
  const matches = filterSlashCommands(raw, commands);
  if (matches.length === 0) {
    return null;
  }

  const query = raw.trim().toLowerCase();
  const exact = matches.find((item) => item.name === query);
  if (exact) {
    return exact;
  }

  const index = Math.max(0, Math.min(matches.length - 1, selectionIndex));
  return matches[index] ?? null;
}
