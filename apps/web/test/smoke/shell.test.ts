import { readFileSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

describe("web shell smoke", () => {
  it("contains all control tabs and panel roots", () => {
    const html = readFileSync(join(process.cwd(), "src/index.html"), "utf8");

    expect(html).toContain('data-tab="chat"');
    expect(html).toContain('data-tab="models"');
    expect(html).toContain('data-tab="envs"');
    expect(html).toContain('data-tab="skills"');
    expect(html).toContain('data-tab="workspace"');
    expect(html).toContain('data-tab="cron"');

    expect(html).toContain('id="panel-chat"');
    expect(html).toContain('id="panel-models"');
    expect(html).toContain('id="models-config-form"');
    expect(html).toContain('id="models-provider-id-select"');
    expect(html).toContain('id="models-provider-name-input"');
    expect(html).toContain('id="panel-envs"');
    expect(html).toContain('id="panel-skills"');
    expect(html).toContain('id="panel-workspace"');
    expect(html).toContain('id="panel-cron"');
  });
});
