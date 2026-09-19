import { Button } from "@heroui/react";
import { Trans } from "@lingui/react/macro";
import { atom, useAtom } from "jotai";

export const darkAtom = atom(false);

export function ThemeToggle() {
  const [dark, setDark] = useAtom(darkAtom);
  return (
    <Button
      variant="outline"
      aria-pressed={dark}
      onPress={() => setDark((current) => !current)}
    >
      <Trans id="theme.toggle">Toggle theme</Trans>
    </Button>
  );
}
