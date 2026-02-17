import { Command } from "commander";
import { ApiClient, ApiClientError } from "../client/api-client.js";
import { printResult } from "../io/output.js";
import { t } from "../i18n.js";

export function registerChatsCommand(program: Command, client: ApiClient): void {
  const chats = program.command("chats").description(t("cli.command.chats"));

  chats
    .command("list")
    .option("--user-id <userId>")
    .option("--channel <channel>")
    .action(async (opts: { userId?: string; channel?: string }) => {
      const qs = new URLSearchParams();
      if (opts.userId) qs.set("user_id", opts.userId);
      if (opts.channel) qs.set("channel", opts.channel);
      const suffix = qs.toString() ? `?${qs.toString()}` : "";
      printResult(await client.get(`/chats${suffix}`));
    });

  chats
    .command("create")
    .requiredOption("--session-id <sid>")
    .requiredOption("--user-id <uid>")
    .option("--channel <channel>", "console")
    .option("--name <name>", t("chats.default_name"))
    .action(async (opts: { sessionId: string; userId: string; channel: string; name: string }) => {
      const payload = {
        name: opts.name,
        session_id: opts.sessionId,
        user_id: opts.userId,
        channel: opts.channel,
        meta: {},
      };
      printResult(await client.post("/chats", payload));
    });

  chats
    .command("delete")
    .argument("<chatId>")
    .action(async (chatId: string) => {
      printResult(await client.delete(`/chats/${encodeURIComponent(chatId)}`));
    });

  chats
    .command("get")
    .argument("<chatId>")
    .action(async (chatId: string) => {
      printResult(await client.get(`/chats/${encodeURIComponent(chatId)}`));
    });

  chats
    .command("send")
    .requiredOption("--chat-session <sid>")
    .requiredOption("--user-id <uid>")
    .option("--channel <channel>", "console")
    .requiredOption("--message <message>")
    .option("--stream")
    .action(async (opts: { chatSession: string; userId: string; channel: string; message: string; stream: boolean }) => {
      const payload = {
        input: [{ role: "user", type: "message", content: [{ type: "text", text: opts.message }] }],
        session_id: opts.chatSession,
        user_id: opts.userId,
        channel: opts.channel,
        stream: Boolean(opts.stream),
      };
      if (!opts.stream) {
        printResult(await client.post("/agent/process", payload));
        return;
      }
      const request = client.buildRequest("/agent/process", {
        method: "POST",
        body: JSON.stringify(payload),
      });
      const res = await fetch(request.url, request.init);
      if (!res.ok || !res.body) {
        const text = await res.text();
        let parsed: unknown = {};
        try {
          parsed = text ? JSON.parse(text) : {};
        } catch {
          parsed = { raw: text };
        }
        const code = (parsed as { error?: { code?: string } })?.error?.code ?? "stream_request_failed";
        const message = (parsed as { error?: { message?: string } })?.error?.message ?? `stream request failed: ${res.status}`;
        const details = (parsed as { error?: { details?: unknown } })?.error?.details;
        throw new ApiClientError({
          status: res.status,
          code,
          message,
          details,
          payload: parsed,
        });
      }
      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        process.stdout.write(decoder.decode(value, { stream: true }));
      }
    });
}
