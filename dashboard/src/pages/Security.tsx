import { IdentitiesPanel } from "@/pages/security/IdentitiesPanel";
import { PasskeysPanel } from "@/pages/security/PasskeysPanel";
import { PasswordTotpPanel } from "@/pages/security/PasswordTotpPanel";
import { SessionsPanel } from "@/pages/security/SessionsPanel";
import { TokensPanel } from "@/pages/security/TokensPanel";

/**
 * Everything about how this account gets in, as one page of stacked sections
 * rather than tabs.
 *
 * The whole page is on screen at once, so every panel mounts with it. That is
 * the point of dropping the strip — nothing is hidden behind a control the
 * reader has to work through — and it is also why each panel brings its own
 * boundary: a block that is still loading, or that failed, says so in its own
 * place instead of taking the page down with it.
 *
 * Each panel draws its own `Section`, so a section's title sits beside the
 * button it names and that button's handler stays with the state it reads.
 * This file owns only the order the sections appear in and the space between
 * them.
 */
export function Security() {
  return (
    <div className="flex flex-col gap-8">
      <PasskeysPanel />
      <PasswordTotpPanel />
      <SessionsPanel />
      <IdentitiesPanel />
      <TokensPanel />
    </div>
  );
}
