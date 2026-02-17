import { describe, expect, it } from "vitest";
import { applyAgentEvent, beginAssistantMessage, historyToViewMessages, settleAssistantMessage } from "../../src/tui/state.js";
import { consumeSSEBuffer, parseAgentStreamData } from "../../src/tui/stream.js";

describe("tui state", () => {
  it("maps history runtime messages into plain view messages", () => {
    const mapped = historyToViewMessages([
      {
        role: "user",
        content: [{ type: "text", text: "hello" }],
      },
      {
        role: "assistant",
        content: [{ type: "text", text: "world" }],
      },
      {
        role: "assistant",
        content: [{ type: "image", text: "ignored" }],
      },
    ]);

    expect(mapped).toEqual([
      { role: "user", text: "hello" },
      { role: "assistant", text: "world" },
    ]);
  });

  it("applies assistant delta and completed events in order", () => {
    const withPending = beginAssistantMessage([{ role: "user", text: "hi" }]);
    const withDelta = applyAgentEvent(withPending, { type: "assistant_delta", delta: "hello " });
    const withMore = applyAgentEvent(withDelta, { type: "assistant_delta", delta: "world" });
    const completed = applyAgentEvent(withMore, { type: "completed", reply: "hello world!" });

    expect(completed.at(-1)).toMatchObject({
      role: "assistant",
      text: "hello world!",
      pending: false,
    });
  });

  it("settles pending assistant message when stream exits without completed", () => {
    const settled = settleAssistantMessage([{ role: "assistant", text: "", pending: true }]);
    expect(settled.at(-1)).toMatchObject({
      role: "assistant",
      text: "(empty)",
      pending: false,
    });
  });
});

describe("tui sse parser", () => {
  it("parses split SSE chunks and done marker", () => {
    let buffer = "data: {\"type\":\"assistant_delta\",\"delta\":\"hel\"}\n\n";
    const first = consumeSSEBuffer(buffer);
    expect(first.events).toHaveLength(1);
    expect(parseAgentStreamData(first.events[0])).toMatchObject({ type: "assistant_delta", delta: "hel" });

    buffer = "data: [DONE]\n\n";
    const second = consumeSSEBuffer(buffer);
    expect(second.events).toHaveLength(1);
    expect(parseAgentStreamData(second.events[0])).toBe("done");
  });
});
