import { cva } from "class-variance-authority";
import { Plus } from "lucide-react";
import type { ReactNode } from "react";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Spinner } from "@/components/ui/spinner";
import { cn } from "@/lib/utils";

/** The fields an account row draws, from the browser's list or a chooser. */
export interface AccountRowAccount {
  displayName: string;
  username: string;
  avatarUrl?: string;
}

/** An account's picture, or its first letter; the rows and the avatar menus share it. */
export function AccountAvatar({
  account,
  className,
}: {
  account: AccountRowAccount;
  className?: string;
}) {
  return (
    <Avatar className={cn("size-8", className)} aria-hidden="true">
      {account.avatarUrl && <AvatarImage src={account.avatarUrl} alt="" />}
      <AvatarFallback>
        {(account.displayName || account.username).slice(0, 1)}
      </AvatarFallback>
    </Avatar>
  );
}

/**
 * The list the rows sit in: the account chooser's, the sign-in limit
 * notice's and the sign-out confirmation's.
 */
export function AccountList({
  label,
  children,
  className,
}: {
  label?: string;
  children: ReactNode;
  className?: string;
}) {
  return (
    <ul
      aria-label={label}
      className={cn(
        "divide-y overflow-hidden rounded-xl border bg-background shadow-xs/5",
        className,
      )}
    >
      {children}
    </ul>
  );
}

const row = cva(
  "@container flex w-full min-w-0 items-center gap-3 px-3.5 py-2.5 text-start text-sm outline-none",
  {
    variants: {
      interactive: {
        true: "cursor-pointer transition-colors hover:bg-accent focus-visible:bg-accent focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset aria-disabled:cursor-default aria-disabled:hover:bg-transparent",
        false: "",
      },
      // Another row's choice is on its way: the rest step back.
      held: { true: "opacity-64", false: "" },
    },
    defaultVariants: { interactive: false, held: false },
  },
);

/**
 * One account in a list. It is a button when `onSelect` is given (the
 * chooser), or a plain row with an `action` at its trailing edge (signing
 * that account out).
 *
 * - `unavailable` is the reason the account cannot be chosen, written in the
 *   row; the row stays in the list, dimmed and unpressable.
 * - `pending` puts the spinner on the chosen row while the page leaves;
 *   `disabled` holds every other row until then.
 * - `tag` is a short label beside the name, such as "Last used".
 */
export function AccountRow({
  account,
  onSelect,
  unavailable,
  pending = false,
  disabled = false,
  tag,
  action,
}: {
  account: AccountRowAccount;
  onSelect?: () => void;
  unavailable?: ReactNode;
  pending?: boolean;
  disabled?: boolean;
  tag?: ReactNode;
  action?: ReactNode;
}) {
  const blocked = unavailable !== undefined;
  const body = (
    <>
      <span className={cn("flex shrink-0", blocked && "opacity-45")}>
        <AccountAvatar account={account} />
      </span>
      {/* In a narrow row the reason goes under the name, which would
          otherwise be squeezed to its first letter. */}
      <span className="flex min-w-0 flex-1 flex-col gap-1 @xs:flex-row @xs:items-center @xs:gap-3">
        <span
          className={cn(
            "grid min-w-0 flex-1 leading-tight",
            blocked && "opacity-45",
          )}
        >
          <span className="truncate font-medium">
            {account.displayName || account.username}
          </span>
          <span className="truncate font-mono text-muted-foreground text-xs">
            {account.username}
          </span>
        </span>
        {blocked && (
          <span className="text-muted-foreground text-xs leading-tight @xs:shrink-0">
            {unavailable}
          </span>
        )}
      </span>
      {tag !== undefined && !blocked && (
        <span className="shrink-0 rounded-full border px-2 text-muted-foreground text-xs">
          {tag}
        </span>
      )}
      {pending && (
        <Spinner
          aria-hidden="true"
          data-slot="spinner"
          className="size-4 shrink-0"
        />
      )}
      {action}
    </>
  );
  if (onSelect === undefined) {
    return (
      <li data-slot="account-row" className={row({ held: disabled })}>
        {body}
      </li>
    );
  }
  const inert = blocked || pending || disabled;
  return (
    <li data-slot="account-row">
      <button
        type="button"
        aria-disabled={inert || undefined}
        aria-busy={pending || undefined}
        onClick={() => {
          if (!inert) onSelect();
        }}
        className={row({ interactive: true, held: disabled && !pending })}
      >
        {body}
      </button>
    </li>
  );
}

/**
 * The last row of the chooser: an account that is not signed in here yet.
 * It follows the rows' pending rules, so a choice in flight holds it too.
 */
export function OtherAccountRow({
  children,
  onSelect,
  pending = false,
  disabled = false,
}: {
  children: ReactNode;
  onSelect: () => void;
  pending?: boolean;
  disabled?: boolean;
}) {
  const inert = pending || disabled;
  return (
    <li data-slot="account-row">
      <button
        type="button"
        aria-disabled={inert || undefined}
        aria-busy={pending || undefined}
        onClick={() => {
          if (!inert) onSelect();
        }}
        className={row({ interactive: true, held: disabled && !pending })}
      >
        <span
          aria-hidden="true"
          className="flex size-8 shrink-0 items-center justify-center rounded-full border border-dashed text-muted-foreground"
        >
          <Plus className="size-4" />
        </span>
        <span className="min-w-0 flex-1 truncate font-medium">{children}</span>
        {pending && (
          <Spinner
            aria-hidden="true"
            data-slot="spinner"
            className="size-4 shrink-0"
          />
        )}
      </button>
    </li>
  );
}
