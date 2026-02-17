import { Command } from "commander";
import { render } from "ink";
import React from "react";
import { ApiClient } from "../client/api-client.js";
import { getLocale, resolveLocale, setLocale } from "../i18n.js";
import { TUIApp } from "../tui/app.js";
import type { TUIBootstrapOptions } from "../tui/types.js";
import { t } from "../i18n.js";

export interface StartTUIOptions {
  client: ApiClient;
  bootstrap: TUIBootstrapOptions;
}

export type StartTUIFn = (options: StartTUIOptions) => Promise<void>;

export async function startTUI(options: StartTUIOptions): Promise<void> {
  const app = render(React.createElement(TUIApp, { client: options.client, bootstrap: options.bootstrap }));
  await app.waitUntilExit();
}

export function registerTUICommand(
  program: Command,
  client: ApiClient,
  hooks?: {
    start?: StartTUIFn;
  },
): void {
  program
    .command("tui")
    .description(t("cli.command.tui.start"))
    .option("--session-id <sessionId>")
    .option("--user-id <userId>")
    .option("--channel <channel>")
    .option("--api-base <apiBase>")
    .option("--api-key <apiKey>")
    .option("--locale <locale>")
    .action(
      async (opts: {
        sessionId?: string;
        userId?: string;
        channel?: string;
        apiBase?: string;
        apiKey?: string;
        locale?: string;
      }) => {
        const locale = resolveLocale(opts.locale ?? getLocale());
        setLocale(locale);

        const tuiClient = new ApiClient({
          base: opts.apiBase ?? client.getBaseURL(),
          apiKey: opts.apiKey ?? client.getAPIKey(),
        });
        const bootstrap: TUIBootstrapOptions = {
          sessionID: opts.sessionId?.trim() || undefined,
          userID: opts.userId?.trim() || process.env.NEXTAI_USER_ID?.trim() || "demo-user",
          channel: opts.channel?.trim() || process.env.NEXTAI_CHANNEL?.trim() || "console",
          apiBase: tuiClient.getBaseURL(),
          apiKey: tuiClient.getAPIKey(),
          locale,
        };

        const runner = hooks?.start ?? startTUI;
        await runner({
          client: tuiClient,
          bootstrap,
        });
      },
    );
}
