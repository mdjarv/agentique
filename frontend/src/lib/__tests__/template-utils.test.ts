import { describe, expect, it } from "vitest";
import type { PromptTemplate } from "~/lib/generated-types";
import { templateHasVariables } from "~/lib/template-utils";

function tmpl(content: string): PromptTemplate {
  return {
    id: "tmpl-1",
    name: "Template",
    description: "",
    content,
    tags: "[]",
    settings: "{}",
    sort_order: 0,
    created_at: "",
    updated_at: "",
  };
}

describe("template-utils", () => {
  it("detects variables repeatedly without regex state leaking between calls", () => {
    expect(templateHasVariables(tmpl("Hello {{name}}"))).toBe(true);
    expect(templateHasVariables(tmpl("Hello {{name}}"))).toBe(true);
  });
});
