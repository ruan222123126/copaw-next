import { readFileSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

describe("web shell smoke", () => {
  it("contains all control tabs and panel roots", () => {
    const html = readFileSync(join(process.cwd(), "src/index.html"), "utf8");

    expect(html).toContain('data-tab="chat"');
    expect(html).toContain('data-tab="search"');
    expect(html).toContain('data-tab="models"');
    expect(html).toContain('data-tab="channels"');
    expect(html).toContain('data-tab="workspace"');
    expect(html).toContain('data-tab="cron"');

    expect(html).toContain('id="panel-chat"');
    expect(html).toContain('id="panel-search"');
    expect(html).toContain('id="search-chat-input"');
    expect(html).toContain('id="search-chat-results"');
    expect(html).toContain('id="panel-models"');
    expect(html).toContain('id="models-active-form"');
    expect(html).toContain('id="models-active-provider-select"');
    expect(html).toContain('id="models-active-model-select"');
    expect(html).toContain('id="models-set-active-btn"');
    expect(html).toContain('id="models-add-provider-btn"');
    expect(html).toContain('id="models-provider-form"');
    expect(html).toContain('id="models-provider-type-select"');
    expect(html).toContain('id="models-provider-name-input"');
    expect(html).toContain('id="panel-channels"');
    expect(html).toContain('id="qq-channel-form"');
    expect(html).toContain('id="qq-channel-api-env"');
    expect(html).toContain('id="panel-workspace"');
    expect(html).toContain('id="workspace-files-body"');
    expect(html).toContain('id="workspace-editor-form"');
    expect(html).toContain('id="workspace-file-content"');
    expect(html).toContain('id="workspace-import-form"');
    expect(html).toContain('id="panel-cron"');
  });
});
