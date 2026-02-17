import { describe, expect, it } from "vitest";

import { DEFAULT_LOCALE, isWebMessageKey, resolveLocale, setLocale, t } from "../../src/i18n";

describe("web i18n", () => {
  it("resolves locale from language tags", () => {
    expect(resolveLocale("zh")).toBe("zh-CN");
    expect(resolveLocale("en-US")).toBe("en-US");
    expect(resolveLocale("fr")).toBe(DEFAULT_LOCALE);
  });

  it("translates with interpolation", () => {
    setLocale("en-US");
    expect(t("status.loadedMessages", { count: 3 })).toBe("Loaded 3 messages");
    expect(t("models.compatible")).toBe("compatible");

    setLocale("zh-CN");
    expect(t("status.chatNotFound", { chatId: "chat-1" })).toBe("未找到会话：chat-1");
    expect(t("models.compatible")).toBe("compatible");
  });

  it("validates i18n keys", () => {
    expect(isWebMessageKey("tab.chat")).toBe(true);
    expect(isWebMessageKey("tab.channels")).toBe(true);
    expect(isWebMessageKey("tab.unknown")).toBe(false);
  });
});
