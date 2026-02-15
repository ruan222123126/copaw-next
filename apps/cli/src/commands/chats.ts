import { Command } from "commander";
import { ApiClient } from "../client/api-client.js";

export function registerChatsCommand(program: Command, client: ApiClient): void {
  const chats = program.command("chats").description("chat management");

  chats
    .command("list")
    .option("--user-id <userId>")
    .option("--channel <channel>")
    .action(async (opts: { userId?: string; channel?: string }) => {
      const qs = new URLSearchParams();
      if (opts.userId) qs.set("user_id", opts.userId);
      if (opts.channel) qs.set("channel", opts.channel);
      const suffix = qs.toString() ? `?${qs.toString()}` : "";
      console.log(JSON.stringify(await client.get(`/chats${suffix}`), null, 2));
    });

  chats
    .command("create")
    .requiredOption("--session-id <sid>")
    .requiredOption("--user-id <uid>")
    .option("--channel <channel>", "console")
    .option("--name <name>", "New Chat")
    .action(async (opts: { sessionId: string; userId: string; channel: string; name: string }) => {
      const payload = {
        name: opts.name,
        session_id: opts.sessionId,
        user_id: opts.userId,
        channel: opts.channel,
        meta: {},
      };
      console.log(JSON.stringify(await client.post("/chats", payload), null, 2));
    });

  chats
    .command("delete")
    .argument("<chatId>")
    .action(async (chatId: string) => {
      console.log(JSON.stringify(await client.delete(`/chats/${encodeURIComponent(chatId)}`), null, 2));
    });

  chats
    .command("get")
    .argument("<chatId>")
    .action(async (chatId: string) => {
      console.log(JSON.stringify(await client.get(`/chats/${encodeURIComponent(chatId)}`), null, 2));
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
        console.log(JSON.stringify(await client.post("/agent/process", payload), null, 2));
        return;
      }
      const base = process.env.COPAW_API_BASE ?? "http://127.0.0.1:8088";
      const res = await fetch(`${base}/agent/process`, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify(payload),
      });
      if (!res.ok || !res.body) {
        throw new Error(`stream request failed: ${res.status}`);
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
