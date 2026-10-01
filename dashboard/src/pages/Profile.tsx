import { AccountPanel } from "@/pages/profile/AccountPanel";
import { AvatarPanel } from "@/pages/profile/AvatarPanel";

/**
 * The account's own page: the picture it shows, then its name and
 * identifiers, as stacked sections rather than tabs. Both are short, and a
 * reader who came to change one often looks at the other.
 *
 * Each panel draws its own `Section`, as on the Security page; this file owns
 * only their order and the space between them.
 */
export function Profile() {
  return (
    <div className="flex flex-col gap-8">
      <AvatarPanel />
      <AccountPanel />
    </div>
  );
}
