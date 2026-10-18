import { useCallback, useEffect, useRef, useState } from "react";

/** How long the check and the "Copied" name stay after a copy. */
const COPIED_MS = 2000;

/** Copies a value to the clipboard, confirming for 2s or reporting a refusal. */
export function useCopyValue(): {
  state: "idle" | "copied" | "failed";
  copy: (value: string) => void;
  confirm: () => void;
} {
  const [state, setState] = useState<"idle" | "copied" | "failed">("idle");
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);

  useEffect(() => () => clearTimeout(timer.current), []);

  const confirm = useCallback(() => {
    clearTimeout(timer.current);
    setState("copied");
    timer.current = setTimeout(() => setState("idle"), COPIED_MS);
  }, []);

  const copy = useCallback(
    (value: string) => {
      navigator.clipboard.writeText(value).then(confirm, () => {
        clearTimeout(timer.current);
        setState("failed");
      });
    },
    [confirm],
  );

  return { state, copy, confirm };
}
