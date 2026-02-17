import type { AgentStreamEvent, RuntimeMessage, TUIMessage } from "./types.js";

function toMessageText(message: RuntimeMessage): string {
  const content = Array.isArray(message.content) ? message.content : [];
  return content
    .filter((item) => item?.type === "text" && typeof item.text === "string")
    .map((item) => item.text?.trim() ?? "")
    .filter((text) => text !== "")
    .join("\n");
}

export function historyToViewMessages(history: RuntimeMessage[]): TUIMessage[] {
  return history
    .map((msg) => {
      const text = toMessageText(msg);
      if (text === "") {
        return null;
      }
      const role: "user" | "assistant" = msg.role === "assistant" ? "assistant" : "user";
      return { role, text };
    })
    .filter((msg): msg is TUIMessage => msg !== null);
}

function ensurePendingAssistant(messages: TUIMessage[]): TUIMessage[] {
  const next = [...messages];
  const last = next.at(-1);
  if (!last || last.role !== "assistant" || !last.pending) {
    next.push({ role: "assistant", text: "", pending: true });
  }
  return next;
}

export function appendUserMessage(messages: TUIMessage[], text: string): TUIMessage[] {
  return [...messages, { role: "user", text }];
}

export function beginAssistantMessage(messages: TUIMessage[]): TUIMessage[] {
  return ensurePendingAssistant(messages);
}

export function applyAgentEvent(messages: TUIMessage[], event: AgentStreamEvent): TUIMessage[] {
  if (!event?.type) {
    return messages;
  }

  if (event.type === "assistant_delta") {
    const next = ensurePendingAssistant(messages);
    const last = next.at(-1);
    if (last) {
      last.text = `${last.text}${typeof event.delta === "string" ? event.delta : ""}`;
    }
    return next;
  }

  if (event.type === "completed") {
    const next = ensurePendingAssistant(messages);
    const last = next.at(-1);
    if (last) {
      if (typeof event.reply === "string" && event.reply.trim() !== "") {
        last.text = event.reply;
      }
      last.pending = false;
    }
    return next;
  }

  if (event.type === "error") {
    const next = ensurePendingAssistant(messages);
    const last = next.at(-1);
    if (last) {
      if (typeof event.meta?.message === "string" && event.meta.message.trim() !== "") {
        last.text = event.meta.message;
      }
      last.pending = false;
      last.failed = true;
    }
    return next;
  }

  return messages;
}

export function settleAssistantMessage(messages: TUIMessage[]): TUIMessage[] {
  const next = [...messages];
  const last = next.at(-1);
  if (last?.role === "assistant" && last.pending) {
    last.pending = false;
    if (last.text.trim() === "") {
      last.text = "(empty)";
    }
  }
  return next;
}
