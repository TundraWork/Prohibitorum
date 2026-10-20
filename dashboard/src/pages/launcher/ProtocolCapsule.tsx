import { useLingui } from "@lingui/react/macro";
import { Blocks, FileKey, Route } from "lucide-react";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";

export type AppKind = "oidc" | "saml" | "forward_auth";

/**
 * How an application signs the member in, as one glyph in a capsule with its
 * plain-language description on hover and focus. The glyphs are the console's
 * own for each kind of application, so the launcher and the management pages
 * speak one vocabulary. The kind is told by glyph and words, never by colour:
 * the console's colours are for state.
 */
export function ProtocolCapsule({
  kind,
  className,
}: {
  kind: AppKind;
  className?: string;
}) {
  const { t } = useLingui();
  const { Icon, name, hint } = {
    oidc: {
      Icon: Blocks,
      name: "OIDC",
      hint: t({
        id: "launcher.kind.oidc",
        message: "Signs you in with OpenID Connect",
      }),
    },
    saml: {
      Icon: FileKey,
      name: "SAML",
      hint: t({ id: "launcher.kind.saml", message: "Signs you in with SAML" }),
    },
    forward_auth: {
      Icon: Route,
      name: t({
        id: "launcher.kind.forward_auth.name",
        message: "Forward auth",
      }),
      hint: t({
        id: "launcher.kind.forward_auth",
        message: "Protected by your sign-in",
      }),
    },
  }[kind];
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <button
            type="button"
            aria-label={`${name} — ${hint}`}
            className={cn(
              "grid size-7 cursor-help place-items-center rounded-full bg-background/85 text-muted-foreground shadow-xs/5 outline-none ring-1 ring-black/8 backdrop-blur-sm transition-colors hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring dark:ring-white/12",
              className,
            )}
          />
        }
      >
        <Icon className="size-3.5" aria-hidden="true" />
      </TooltipTrigger>
      <TooltipPopup>
        <span className="flex flex-col gap-0.5">
          <span className="font-mono text-xs">{name}</span>
          <span>{hint}</span>
        </span>
      </TooltipPopup>
    </Tooltip>
  );
}
