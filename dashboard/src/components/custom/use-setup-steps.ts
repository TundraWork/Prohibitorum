import { useEffect, useRef, useState } from "react";

/**
 * The two steps of setting up a password and an authenticator in one form:
 * which is showing, and the panes that hold each.
 *
 * Validators and the submit handler read the step through `stepRef`, so a
 * handler created before the step changed still sees the new one. Both panes
 * stay mounted so the form keeps every value; `useFocusOnStep` puts focus in
 * the field the new step starts with.
 */
export function useSetupSteps() {
  const [step, setStep] = useState<"password" | "authenticator">("password");
  const stepRef = useRef(step);
  const moved = useRef(false);
  const passwordPane = useRef<HTMLDivElement>(null);
  const authenticatorPane = useRef<HTMLDivElement>(null);

  const go = (next: "password" | "authenticator") => {
    stepRef.current = next;
    moved.current = true;
    setStep(next);
  };

  return { step, stepRef, moved, go, passwordPane, authenticatorPane };
}

/**
 * A step change takes the control that caused it away, so focus moves to the
 * field the new step starts with: the first password, or the code. It waits for
 * a submit to settle, because the fields are disabled until then and a
 * disabled field does not take focus.
 */
export function useFocusOnStep(
  steps: ReturnType<typeof useSetupSteps>,
  submitting: boolean,
) {
  const { step, moved, passwordPane, authenticatorPane } = steps;
  useEffect(() => {
    if (!moved.current || submitting) return;
    moved.current = false;
    const target =
      step === "password"
        ? passwordPane.current?.querySelector("input")
        : authenticatorPane.current?.querySelector<HTMLInputElement>(
            'input[autocomplete="one-time-code"]',
          );
    target?.focus();
  }, [step, submitting, moved, passwordPane, authenticatorPane]);
}
