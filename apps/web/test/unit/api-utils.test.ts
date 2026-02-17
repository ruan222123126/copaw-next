import { describe, expect, it } from "vitest";

import { parseEnvMap, parseErrorMessage } from "../../src/api-utils";

describe("parseErrorMessage", () => {
  it("returns code and message when error envelope is present", () => {
    const raw = JSON.stringify({ error: { code: "invalid_json", message: "invalid request body" } });
    expect(parseErrorMessage(raw, 400)).toBe("invalid_json: invalid request body");
  });

  it("returns fallback for empty response body", () => {
    expect(parseErrorMessage("", 502)).toBe("请求失败（502）");
  });

  it("supports localized fallback when provided", () => {
    expect(parseErrorMessage("", 502, "Request failed (502)")).toBe("Request failed (502)");
  });

  it("returns raw text when payload is not json", () => {
    expect(parseErrorMessage("upstream timeout", 504)).toBe("upstream timeout");
  });
});

describe("parseEnvMap", () => {
  it("parses valid env map", () => {
    expect(parseEnvMap('{"OPENAI_API_KEY":"sk-xxx","NEXTAI_MODE":"dev"}')).toEqual({
      OPENAI_API_KEY: "sk-xxx",
      NEXTAI_MODE: "dev",
    });
  });

  it("allows empty payload as empty object", () => {
    expect(parseEnvMap("   ")).toEqual({});
  });

  it("throws on non-string value", () => {
    expect(() => parseEnvMap('{"PORT":8080}')).toThrow("invalid_env_value: 键 PORT 的值必须是字符串");
  });

  it("supports custom error messages", () => {
    expect(
      () =>
        parseEnvMap('{"PORT":8080}', {
          invalidJSON: "bad json",
          invalidMap: "bad map",
          invalidKey: "bad key",
          invalidValue: (key) => `bad value: ${key}`,
        }),
    ).toThrow("bad value: PORT");
  });
});
