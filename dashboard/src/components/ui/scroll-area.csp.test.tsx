import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ScrollArea } from "./scroll-area";

/**
 * Base UI hides the native scrollbars with a style element it injects at
 * runtime. The server's Content-Security-Policy allows inline styles only by
 * hash, so a blocked rule shows the browser's scrollbars on every scroll area.
 * This fails when an upgrade changes the rule and the policy is not updated.
 */
describe("the scroll area's injected style", () => {
  it("is allowed by the server's style policy", () => {
    render(<ScrollArea>content</ScrollArea>);
    const style = Array.from(document.querySelectorAll("style")).find((node) =>
      node.textContent?.includes("base-ui-disable-scrollbar"),
    );
    expect(style).toBeDefined();
    const hash = createHash("sha256")
      .update(style?.textContent ?? "")
      .digest("base64");
    const server = readFileSync(
      resolve(process.cwd(), "../pkg/webui/webui.go"),
      "utf8",
    );
    expect(server).toContain(`'sha256-${hash}'`);
  });
});
