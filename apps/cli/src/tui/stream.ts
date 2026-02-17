import type { AgentStreamEvent } from "./types.js";

export interface SSEReadResult {
  events: string[];
  rest: string;
}

export function consumeSSEBuffer(buffer: string): SSEReadResult {
  const events: string[] = [];
  let rest = buffer;

  while (true) {
    const boundary = rest.indexOf("\n\n");
    if (boundary < 0) {
      break;
    }

    const block = rest.slice(0, boundary);
    rest = rest.slice(boundary + 2);

    const dataLines = block
      .split("\n")
      .map((line) => line.trimEnd())
      .filter((line) => line.startsWith("data:"))
      .map((line) => line.slice(5).trimStart());

    if (dataLines.length > 0) {
      events.push(dataLines.join("\n"));
    }
  }

  return { events, rest };
}

export function parseAgentStreamData(data: string): AgentStreamEvent | "done" | null {
  if (data === "[DONE]") {
    return "done";
  }
  try {
    return JSON.parse(data) as AgentStreamEvent;
  } catch {
    return null;
  }
}
