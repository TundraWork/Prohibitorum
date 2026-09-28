import { Tooltip } from "@heroui/react";
import { useLingui } from "@lingui/react/macro";
import { Check, ClipboardCopy } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { Button } from "@/components/custom/Button";
import { ScrollArea } from "@/components/custom/ScrollArea";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";

/** How long the check and "Copied" stay on screen after a copy. */
const COPIED_MS = 2000;

/**
 * Indents compacted JSON text two spaces a level, without parsing it.
 *
 * The server sends upstream documents compacted and as text so that numbers
 * too large for a double, and the provider's key order, arrive intact;
 * `JSON.parse` would round the first and could reorder the second. This walks
 * the characters instead and only acts on structure outside strings, so every
 * value is copied through exactly as it was written.
 */
export function formatJson(text: string): string {
  let out = "";
  let depth = 0;
  let inString = false;
  let escaped = false;
  const newline = () => `\n${"  ".repeat(depth)}`;
  for (let i = 0; i < text.length; i++) {
    const c = text[i];
    if (inString) {
      out += c;
      if (escaped) escaped = false;
      else if (c === "\\") escaped = true;
      else if (c === '"') inString = false;
      continue;
    }
    switch (c) {
      case '"':
        inString = true;
        out += c;
        break;
      case "{":
      case "[": {
        const close = c === "{" ? "}" : "]";
        if (text[i + 1] === close) {
          out += c + close;
          i++;
          break;
        }
        depth++;
        out += c + newline();
        break;
      }
      case "}":
      case "]":
        depth--;
        out += newline() + c;
        break;
      case ",":
        out += `,${newline()}`;
        break;
      case ":":
        out += ": ";
        break;
      case " ":
      case "\t":
      case "\n":
      case "\r":
        break;
      default:
        out += c;
    }
  }
  return out;
}

/**
 * A JSON document on a dialog, indented, with a copy button at its corner.
 *
 * The block is drawn the way the public-key dialog draws a JWK: the surface
 * and radius sit on the `ScrollArea` host so a long line scrolls sideways over
 * the same fill, and lines never wrap, which would break the structure. The
 * block does not cap its own height; the dialog scrolls. What is copied is the
 * indented text on screen.
 */
export function JsonBlock({ json }: { json: string }) {
  const { t } = useLingui();
  const text = useMemo(() => formatJson(json), [json]);
  const [state, setState] = useState<"idle" | "copied" | "failed">("idle");
  const [hovered, setHovered] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);

  useEffect(() => () => clearTimeout(timer.current), []);

  const copy = () => {
    clearTimeout(timer.current);
    navigator.clipboard.writeText(text).then(
      () => {
        setState("copied");
        timer.current = setTimeout(() => setState("idle"), COPIED_MS);
      },
      () => setState("failed"),
    );
  };

  const copied = state === "copied";
  const label = copied
    ? t({ id: "json-block.copied", message: "Copied" })
    : t({ id: "json-block.copy", message: "Copy" });

  return (
    <div className="flex flex-col gap-3">
      <div className="relative">
        <ScrollArea className="rounded-[0.375rem] bg-surface-secondary p-3 pr-11 font-mono text-xs">
          <pre>{text}</pre>
        </ScrollArea>
        <div className="absolute top-1.5 right-1.5">
          <Tooltip
            delay={0}
            isOpen={hovered || copied}
            onOpenChange={setHovered}
          >
            <Button
              isIconOnly
              size="sm"
              variant="ghost"
              aria-label={label}
              onPress={copy}
            >
              {copied ? (
                <Check size={14} aria-hidden="true" />
              ) : (
                <ClipboardCopy size={14} aria-hidden="true" />
              )}
            </Button>
            <Tooltip.Content>{label}</Tooltip.Content>
          </Tooltip>
        </div>
      </div>
      <span role="status" className="sr-only">
        {copied ? label : ""}
      </span>
      {state === "failed" && (
        <SurfaceAlert status="danger" role="alert">
          <SurfaceAlert.Indicator />
          <SurfaceAlert.Content>
            <SurfaceAlert.Title>
              {t({
                id: "json-block.copy.failed",
                message: "Could not copy. Your browser blocked the clipboard.",
              })}
            </SurfaceAlert.Title>
          </SurfaceAlert.Content>
        </SurfaceAlert>
      )}
    </div>
  );
}
