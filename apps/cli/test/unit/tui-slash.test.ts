import { describe, expect, it } from "vitest";
import { filterSlashCommands, isSlashDraft, resolveSlashCommand } from "../../src/tui/slash.js";

describe("tui slash commands", () => {
  it("recognizes slash draft", () => {
    expect(isSlashDraft("/h")).toBe(true);
    expect(isSlashDraft(" /history")).toBe(true);
    expect(isSlashDraft("hello")).toBe(false);
  });

  it("filters command list by prefix", () => {
    const matches = filterSlashCommands("/re");
    expect(matches.map((item) => item.name)).toEqual(["/refresh"]);
  });

  it("resolves exact and selected command", () => {
    expect(resolveSlashCommand("/history", 0)?.name).toBe("/history");
    expect(resolveSlashCommand("/r", 0)?.name).toBe("/refresh");
    expect(resolveSlashCommand("/r", 100)?.name).toBe("/refresh");
    expect(resolveSlashCommand("/unknown", 0)).toBeNull();
  });
});
