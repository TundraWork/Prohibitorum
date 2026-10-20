import { Trans, useLingui } from "@lingui/react/macro";
import {
  QueryErrorResetBoundary,
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { CatchBoundary } from "@tanstack/react-router";
import { LayoutGrid, Plus, SearchIcon } from "lucide-react";
import { Suspense, useEffect, useRef, useState } from "react";
import { revokeConsentMutationOptions } from "@/api/mutations";
import {
  consentQueryOptions,
  myAppsQueryOptions,
  sessionQueryOptions,
} from "@/api/queries";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { SectionFailure } from "@/components/custom/Section";
import {
  Dialog,
  DialogDescription,
  DialogHeader,
  DialogPanel,
  DialogPopup,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty";
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from "@/components/ui/input-group";
import { Kbd } from "@/components/ui/kbd";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import {
  AppIcon,
  AppTile,
  coverStyle,
  isAppKind,
  type LaunchpadApp,
} from "@/pages/launcher/AppTile";
import {
  type AppKind,
  ProtocolCapsule,
} from "@/pages/launcher/ProtocolCapsule";

/** Tiles in columns that fill the page, none narrower than a name needs. */
const pad =
  "grid grid-cols-[repeat(auto-fill,minmax(min(100%,9rem),1fr))] gap-3 sm:grid-cols-[repeat(auto-fill,minmax(11rem,1fr))] sm:gap-4";

/**
 * Where a member lands after signing in: the applications they have connected,
 * one tile each, and the way to connect the others.
 *
 * Connected means the application can be opened without being asked anything
 * first: a forward-auth application or an OIDC one that asks for no consent is
 * connected as soon as the member may use it; any other is connected once the
 * member has allowed it. The rest wait behind Add app, and opening one from
 * there is how it gets connected.
 */
export function Launcher() {
  const { t } = useLingui();
  const { data: session } = useSuspenseQuery({
    ...sessionQueryOptions(),
    refetchOnMount: false,
  });
  const [query, setQuery] = useState("");
  const search = useRef<HTMLInputElement>(null);
  useSearchShortcut(search);
  if (session === null) return null;
  const firstName =
    session.displayName.trim().split(/\s+/)[0] || session.username;

  return (
    <div className="flex flex-col gap-8">
      <header className="flex flex-col gap-5 sm:flex-row sm:items-end sm:justify-between">
        <div className="flex min-w-0 flex-col gap-2">
          <h1 className="text-balance font-semibold text-3xl tracking-tight sm:text-4xl">
            <Greeting name={firstName} />{" "}
            <span aria-hidden="true" className="inline-block">
              👋
            </span>
          </h1>
          {/* keep-all breaks Chinese only at its punctuation, never inside a
              word; Latin text wraps as usual. */}
          <p className="text-pretty text-muted-foreground [word-break:keep-all]">
            <Trans id="launcher.tagline">
              Quick access to your connected applications. Choose Add app to
              connect more.
            </Trans>
          </p>
        </div>
        <InputGroup className="w-full bg-background sm:w-80 dark:bg-background">
          <InputGroupAddon>
            <SearchIcon aria-hidden="true" />
          </InputGroupAddon>
          <InputGroupInput
            ref={search}
            type="search"
            value={query}
            autoComplete="off"
            onChange={(event) => setQuery(event.target.value)}
            placeholder={t({
              id: "launcher.search.placeholder",
              message: "Search your apps",
            })}
            aria-label={t({
              id: "launcher.search",
              message: "Search your apps",
            })}
            aria-keyshortcuts={appleKeys ? "Meta+K /" : "Control+K /"}
          />
          <InputGroupAddon align="inline-end" className="max-sm:hidden">
            <Kbd className="whitespace-nowrap">
              {appleKeys ? "⌘K" : "Ctrl K"}
            </Kbd>
          </InputGroupAddon>
        </InputGroup>
      </header>

      <QueryErrorResetBoundary>
        {({ reset: resetQueries }) => (
          <CatchBoundary
            getResetKey={() => "launcher"}
            errorComponent={({ error, reset }) => (
              <SectionFailure
                error={error}
                title={
                  <Trans id="launcher.failed">
                    Your applications could not be loaded
                  </Trans>
                }
                onRetry={() => {
                  resetQueries();
                  reset();
                }}
              />
            )}
          >
            <Suspense fallback={<PadPending />}>
              <Pad query={query} isAdmin={session.role === "admin"} />
            </Suspense>
          </CatchBoundary>
        )}
      </QueryErrorResetBoundary>
    </div>
  );
}

function Pad({ query, isAdmin }: { query: string; isAdmin: boolean }) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  // Both reads decide the page: without the grants, every connected
  // application would fall back behind Add app. A failure of either is the
  // page's failure, not a quieter page.
  const { data: apps } = useSuspenseQuery(myAppsQueryOptions());
  const { data: grants } = useSuspenseQuery(consentQueryOptions());
  const [adding, setAdding] = useState(false);
  const arriving = useArrival();
  const [removing, setRemoving] = useState<LaunchpadApp | null>(null);
  const revoke = useMutation(revokeConsentMutationOptions(queryClient));

  const granted = new Set(
    grants.map((grant) => `${grant.kind}:${grant.clientId}`),
  );
  const known = apps.flatMap((app) =>
    isAppKind(app.kind) ? [{ app, kind: app.kind }] : [],
  );
  const hasGrant = (app: LaunchpadApp, kind: AppKind) =>
    kind !== "forward_auth" && granted.has(`${kind}:${app.id}`);
  // An OIDC application that asks for no consent may still have an old grant
  // on record; it has nothing to remove, because removing would change nothing.
  const consentFree = (app: LaunchpadApp, kind: AppKind) =>
    kind === "forward_auth" ||
    (kind === "oidc" && app.requireConsent === false);
  const connected = known.filter(
    ({ app, kind }) => consentFree(app, kind) || hasGrant(app, kind),
  );
  const available = known.filter(
    ({ app, kind }) => !consentFree(app, kind) && !hasGrant(app, kind),
  );

  const needle = query.trim().toLocaleLowerCase();
  const shown =
    needle === ""
      ? connected
      : connected.filter(({ app }) =>
          app.name.toLocaleLowerCase().includes(needle),
        );

  if (known.length === 0) {
    return (
      <Empty className="rounded-[22px] border border-dashed">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <LayoutGrid aria-hidden="true" />
          </EmptyMedia>
          <EmptyTitle>
            <Trans id="launcher.empty.title">No applications yet</Trans>
          </EmptyTitle>
          <EmptyDescription>
            <Trans id="launcher.empty.description">
              When an administrator gives you access to an application, it
              appears here.
            </Trans>
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    );
  }

  return (
    <>
      <ul
        className={cn(pad, arriving && "launcher-arrive")}
        aria-label={t({ id: "launcher.list", message: "Your applications" })}
      >
        {shown.map(({ app, kind }, order) => (
          <AppTile
            key={`${kind}:${app.id}`}
            order={order}
            app={app}
            kind={kind}
            granted={hasGrant(app, kind) && !consentFree(app, kind)}
            canRevoke={hasGrant(app, kind) && !consentFree(app, kind)}
            canManage={isAdmin}
            onRevoke={() => setRemoving(app)}
          />
        ))}
        {available.length > 0 && needle === "" && (
          <li>
            {/* The tile's own proportions in outline: a cover-sized well and
                a name band, so it closes the pad as one more key. */}
            <button
              type="button"
              onClick={() => setAdding(true)}
              className="group/add flex size-full cursor-pointer flex-col rounded-[22px] border border-input border-dashed bg-sidebar/90 p-1.5 text-muted-foreground outline-none transition-colors hover:border-ring hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background"
            >
              <span className="flex aspect-4/3 flex-col items-center justify-center gap-1.5 rounded-2xl transition-colors group-hover/add:bg-accent/60">
                <Plus className="size-7" aria-hidden="true" />
                <span className="text-caption">
                  <Trans id="launcher.add.hint">Connect more</Trans>
                </span>
              </span>
              <span className="flex min-h-10 flex-col items-start justify-center px-2.5 pt-1.5 text-start">
                <span className="font-medium text-foreground text-sm">
                  <Trans id="launcher.add">Add app</Trans>
                </span>
              </span>
            </button>
          </li>
        )}
      </ul>

      {needle !== "" && shown.length === 0 && (
        <p role="status" className="text-muted-foreground text-sm">
          <Trans id="launcher.search.none">No apps match “{query}”.</Trans>
        </p>
      )}
      {needle === "" && connected.length === 0 && (
        <p role="status" className="text-muted-foreground text-sm">
          <Trans id="launcher.connect_first">
            Connect an app to get started.
          </Trans>
        </p>
      )}

      <AddAppDialog open={adding} onOpenChange={setAdding} apps={available} />

      <ConfirmDialog
        isOpen={removing !== null}
        onOpenChange={(open) => !open && !revoke.isPending && setRemoving(null)}
        status="danger"
        title={<Trans id="launcher.revoke.title">Remove access?</Trans>}
        subject={removing?.name}
        body={
          <p>
            <Trans id="launcher.revoke.body">
              It will ask for your permission again the next time you sign in to
              it.
            </Trans>
          </p>
        }
        confirmLabel={<Trans id="launcher.revoke">Remove access</Trans>}
        isPending={revoke.isPending}
        onConfirm={() => {
          if (removing === null || !isAppKind(removing.kind)) return;
          // The kind tells the server an OIDC client id from a SAML
          // application's id, which could otherwise collide.
          revoke.mutate(
            { kind: removing.kind, clientId: removing.id },
            { onSettled: () => setRemoving(null) },
          );
        }}
      />
    </>
  );
}

/**
 * The applications the member may use but has not connected. Opening one
 * signs them in to it, which is what connects it.
 */
function AddAppDialog({
  open,
  onOpenChange,
  apps,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  apps: { app: LaunchpadApp; kind: AppKind }[];
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogPopup>
        <DialogHeader>
          <DialogTitle>
            <Trans id="launcher.add.title">Connect an app</Trans>
          </DialogTitle>
          <DialogDescription>
            <Trans id="launcher.add.description">
              Open one to sign in to it. It joins your apps once you have.
            </Trans>
          </DialogDescription>
        </DialogHeader>
        <DialogPanel>
          <ul className="flex flex-col gap-1">
            {apps.map(({ app, kind }) => (
              <li
                key={`${kind}:${app.id}`}
                className="relative flex items-center gap-3 rounded-lg p-2 transition-colors hover:bg-accent"
              >
                <a
                  href={app.launchUrl}
                  target="_blank"
                  rel="noopener noreferrer"
                  onClick={() => onOpenChange(false)}
                  className="absolute inset-0 rounded-lg outline-none focus-visible:ring-2 focus-visible:ring-ring"
                >
                  <span className="sr-only">
                    <Trans id="launcher.open">
                      Open {app.name} in a new tab
                    </Trans>
                  </span>
                </a>
                {/* The tile the application will become, in small. */}
                <span
                  style={coverStyle(app)}
                  className="app-cover-tint grid size-9 shrink-0 place-items-center rounded-[10px] border border-black/6 dark:border-white/6"
                >
                  <span className="@container size-[72%]">
                    <AppIcon app={app} />
                  </span>
                </span>
                <span className="min-w-0 flex-1 truncate font-medium text-sm">
                  {app.name}
                </span>
                <ProtocolCapsule kind={kind} className="relative" />
              </li>
            ))}
          </ul>
        </DialogPanel>
      </DialogPopup>
    </Dialog>
  );
}

/** The pad settles in once per page load, not each time it is shown. */
let arrived = false;
const arrivalMs = 1100;

function useArrival() {
  const [arriving, setArriving] = useState(!arrived);
  useEffect(() => {
    arrived = true;
    if (!arriving) return;
    const timer = setTimeout(() => setArriving(false), arrivalMs);
    return () => clearTimeout(timer);
  }, [arriving]);
  return arriving;
}

/** Hello for the reader's time of day, by the clock of the device they use. */
function Greeting({ name }: { name: string }) {
  const hour = new Date().getHours();
  if (hour >= 5 && hour < 12) {
    return <Trans id="launcher.greeting.morning">Good morning, {name}</Trans>;
  }
  if (hour >= 12 && hour < 18) {
    return (
      <Trans id="launcher.greeting.afternoon">Good afternoon, {name}</Trans>
    );
  }
  return <Trans id="launcher.greeting.evening">Good evening, {name}</Trans>;
}

/** Apple keyboards say ⌘ where others say Ctrl; the shortcut takes either. */
const appleKeys =
  typeof navigator !== "undefined" &&
  /mac|iphone|ipad|ipod/i.test(navigator.platform || navigator.userAgent);

/** ⌘K or Ctrl-K anywhere, or / when not typing, goes to the search field. */
function useSearchShortcut(field: React.RefObject<HTMLInputElement | null>) {
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null;
      const typing =
        target !== null &&
        (target.tagName === "INPUT" ||
          target.tagName === "TEXTAREA" ||
          target.isContentEditable);
      const chord =
        (event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k";
      if (chord || (event.key === "/" && !typing)) {
        event.preventDefault();
        field.current?.focus();
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [field]);
}

/** Tiles about to arrive, in the pad they will fill. */
const placeholders = ["a", "b", "c", "d", "e", "f"];

function PadPending() {
  return (
    <ul aria-hidden="true" className={pad}>
      {placeholders.map((slot) => (
        <li
          key={slot}
          className="rounded-[22px] bg-(--frame-band) p-1.5 ring-1 ring-border"
        >
          <Skeleton className="aspect-4/3 w-full rounded-2xl" />
          <Skeleton className="mx-2.5 my-3 h-4 w-3/5" />
        </li>
      ))}
    </ul>
  );
}
