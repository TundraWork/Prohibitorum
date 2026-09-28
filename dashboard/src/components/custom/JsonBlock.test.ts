import { describe, expect, it } from "vitest";
import { formatJson } from "@/components/custom/JsonBlock";

describe("formatJson", () => {
  it("indents nested objects and arrays two spaces a level", () => {
    expect(formatJson('{"a":1,"b":{"c":[true,null]}}')).toBe(
      [
        "{",
        '  "a": 1,',
        '  "b": {',
        '    "c": [',
        "      true,",
        "      null",
        "    ]",
        "  }",
        "}",
      ].join("\n"),
    );
  });

  it("keeps empty objects and arrays on one line", () => {
    expect(formatJson('{"groups":[],"address":{}}')).toBe(
      '{\n  "groups": [],\n  "address": {}\n}',
    );
  });

  it("leaves structure characters and escapes inside strings alone", () => {
    const text = '{"note":"say \\"hi\\" {, } [:]","path":"C:\\\\dir\\\\"}';
    expect(formatJson(text)).toBe(
      '{\n  "note": "say \\"hi\\" {, } [:]",\n  "path": "C:\\\\dir\\\\"\n}',
    );
  });

  it("copies numbers through as written", () => {
    expect(formatJson('{"id":9007199254740993,"f":1.50e+3}')).toBe(
      '{\n  "id": 9007199254740993,\n  "f": 1.50e+3\n}',
    );
  });
});
