import { linkVariants, Skeleton, Spinner } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { browserSupportsWebAuthn } from "@simplewebauthn/browser";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link as RouterLink, useRouter } from "@tanstack/react-router";
import { type ReactNode, useEffect, useRef, useState } from "react";
import { ApiError, isCancellation } from "@/api/errors";
import {
  pairingCompleteMutationOptions,
  pairingStartMutationOptions,
  setupPasskeyMutationOptions,
} from "@/api/mutations";
import { clearSessionQueries, pairingStatusQueryOptions } from "@/api/queries";
import type { PairingStart } from "@/api/raw-paths";
import { followRedirect } from "@/app/redirect";
import { Button } from "@/components/custom/Button";
import { useInstanceBranding } from "@/components/custom/instance-branding";
import { PublicStep } from "@/components/custom/PublicStep";
import { QrCode } from "@/components/custom/QrCode";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";
import { PasskeyButton } from "@/pages/public/enroll/PasskeyButton";
import { Route } from "@/routes/_public.pair";

/** The drawn size of the approval link's QR code, quiet zone included. */
const qrSize = 200;

/**
 * Signing this device in from another one. The page asks the server for a
 * pairing as soon as it opens, shows its code, and waits: the reader types
 * the code — or scans the link — on a device that is signed in, and approves
 * it there. Once approved, this device signs itself in and offers to add a
 * passkey, so the next sign-in here needs no other device.
 *
 * The pairing is started here rather than in the loader, which hover
 * preloading runs for a link the reader only pointed at. A code that runs out
 * is replaced on request, never on its own.
 */
export function PairPage() {
  const { return_to } = Route.useSearch();
  // The loader has refused a repeated value already.
  const returnTo = Array.isArray(return_to) ? undefined : return_to;
  const start = useMutation(pairingStartMutationOptions());
  // `generation` counts the codes that replaced an earlier one.
  const [current, setCurrent] = useState<{
    pairing: PairingStart;
    generation: number;
  }>();
  const [signedIn, setSignedIn] = useState<string>();

  const begin = async () => {
    try {
      const pairing = await start.mutateAsync();
      setCurrent((previous) => ({
        pairing,
        generation: previous === undefined ? 0 : previous.generation + 1,
      }));
    } catch {
      // The toast has reported it; the page offers a new code.
    }
  };

  // Once per page: a development re-run of the effect must not start a
  // second pairing, which would leave the first one's code on screen.
  const started = useRef(false);
  useEffect(() => {
    if (started.current) return;
    started.current = true;
    void begin();
  });

  if (signedIn !== undefined) return <SignedIn redirect={signedIn} />;
  return (
    <PairingStep
      // A code that replaces another starts the step over, and its title takes
      // focus again. The first code fills in the step the page opened with.
      key={current?.generation ?? 0}
      pairing={current?.pairing}
      returnTo={returnTo}
      starting={start.isPending}
      onRestart={() => void begin()}
      onSignedIn={setSignedIn}
    />
  );
}

function secondsUntil(deadline: number): number {
  return Math.max(0, Math.ceil((deadline - Date.now()) / 1000));
}

/** Whole seconds left until `expiresAt`, counted down once a second. */
function useSecondsLeft(expiresAt: string | undefined): number | undefined {
  const deadline = expiresAt === undefined ? undefined : Date.parse(expiresAt);
  const [seconds, setSeconds] = useState(() =>
    deadline === undefined ? undefined : secondsUntil(deadline),
  );
  useEffect(() => {
    if (deadline === undefined) return;
    setSeconds(secondsUntil(deadline));
    const timer = window.setInterval(() => {
      const next = secondsUntil(deadline);
      setSeconds(next);
      if (next === 0) window.clearInterval(timer);
    }, 1000);
    return () => window.clearInterval(timer);
  }, [deadline]);
  return seconds;
}

function formatSeconds(total: number): string {
  const minutes = Math.floor(total / 60);
  const seconds = total % 60;
  return `${minutes}:${String(seconds).padStart(2, "0")}`;
}

/**
 * One pairing, from its code to the approval. Keyed by the pairing, so a new
 * code starts it over.
 *
 * It waits while the server says the pairing is pending and the code has
 * time left. An approval is completed at once; a pairing that the server
 * reports expired, that runs out on the clock, or that completing finds gone
 * is shown as expired, with the offer of a new code. The countdown is not
 * announced as it runs; the expiry and the approval are, once each.
 */
function PairingStep({
  pairing,
  returnTo,
  starting,
  onRestart,
  onSignedIn,
}: {
  pairing: PairingStart | undefined;
  returnTo: string | undefined;
  starting: boolean;
  onRestart: () => void;
  onSignedIn: (redirect: string) => void;
}) {
  const { t } = useLingui();
  const { name: instance } = useInstanceBranding();
  const queryClient = useQueryClient();
  const secondsLeft = useSecondsLeft(pairing?.expiresAt);
  // `claimed` once the approval has been seen and completing has begun: the
  // pairing is not read again, since a used-up pairing reads as expired.
  const [claimed, setClaimed] = useState(false);
  // `gone` once completing found the pairing used up or run out.
  const [gone, setGone] = useState(false);
  const outOfTime = secondsLeft === 0;
  const status = useQuery({
    ...pairingStatusQueryOptions(pairing?.pairingId ?? ""),
    enabled: pairing !== undefined && !claimed && !gone && !outOfTime,
  });
  const complete = useMutation(pairingCompleteMutationOptions(returnTo));

  const approved = (claimed || status.data?.status === "approved") && !gone;
  const expired =
    pairing !== undefined &&
    (gone || (!approved && (outOfTime || status.data?.status === "expired")));

  const finish = async (pairingId: string) => {
    try {
      const result = await complete.mutateAsync(pairingId);
      if (result.outcome === "expired") {
        setGone(true);
        return;
      }
      await clearSessionQueries(queryClient);
      onSignedIn(result.redirect);
    } catch {
      // The toast has reported it; the page offers to try again.
    }
  };

  // Completed once, the moment the approval is seen. A failure waits for the
  // reader's retry rather than trying again on the next read.
  useEffect(() => {
    if (!approved || claimed || !pairing) return;
    setClaimed(true);
    void finish(pairing.pairingId);
  });

  // Drawn once each, when the pairing reaches the state; the countdown in the
  // visible status line is never part of it.
  const announcement = expired
    ? t({ id: "pair.expired", message: "This pairing code has expired." })
    : approved
      ? t({ id: "pair.approved", message: "Approved. Signing in…" })
      : "";

  let actions: ReactNode;
  if (pairing === undefined ? !starting : expired) {
    actions = (
      <PublicStep.Actions>
        <Button fullWidth isPending={starting} onPress={onRestart}>
          <Trans id="pair.restart">Get a new code</Trans>
        </Button>
      </PublicStep.Actions>
    );
  } else if (approved && complete.isError) {
    actions = (
      <PublicStep.Actions>
        <Button
          fullWidth
          onPress={() => pairing && void finish(pairing.pairingId)}
        >
          <Trans id="pair.retry">Try again</Trans>
        </Button>
      </PublicStep.Actions>
    );
  }

  return (
    <PublicStep
      title={<Trans id="pair.title">Sign in with another device</Trans>}
      description={
        <Trans id="pair.description">
          On a device that's already signed in to {instance}, open Devices and
          enter the code below.
        </Trans>
      }
      actions={
        <>
          {actions}
          {(!approved || complete.isError) && (
            <p className="text-center text-sm">
              <RouterLink
                to="/login"
                search={returnTo === undefined ? {} : { return_to: returnTo }}
                className={linkVariants().base()}
              >
                <Trans id="pair.back">Back to sign in</Trans>
              </RouterLink>
            </p>
          )}
        </>
      }
    >
      <p className="sr-only" role="status">
        {announcement}
      </p>
      <div
        className={`flex flex-col items-center gap-2 ${expired ? "opacity-50" : ""}`}
      >
        {pairing === undefined ? (
          starting && (
            <>
              <Skeleton className="h-10 w-52 rounded-lg" />
              {/* The QR code's own size, `qrSize`. */}
              <Skeleton className="size-[200px] max-w-full rounded-lg" />
            </>
          )
        ) : (
          <>
            <p className="font-mono text-3xl font-semibold tracking-widest tabular-nums">
              <span className="sr-only">{[...pairing.code].join(" ")}</span>
              <span aria-hidden="true">{pairing.displayCode}</span>
            </p>
            <QrCode
              value={`${window.location.origin}/devices?${new URLSearchParams({ code: pairing.code })}`}
              label={t({
                id: "pair.qr",
                message: "QR code that opens this pairing on the other device",
              })}
              size={qrSize}
            />
            {!expired && (
              <p className="text-center text-sm text-muted">
                <Trans id="pair.qr_hint">
                  Or scan it with that device to open it there.
                </Trans>
              </p>
            )}
          </>
        )}
      </div>
      {expired ? (
        <SurfaceAlert status="warning">
          <SurfaceAlert.Indicator />
          <SurfaceAlert.Content>
            <SurfaceAlert.Title>
              <Trans id="pair.expired">This pairing code has expired.</Trans>
            </SurfaceAlert.Title>
          </SurfaceAlert.Content>
        </SurfaceAlert>
      ) : (
        pairing !== undefined &&
        secondsLeft !== undefined && (
          <p className="flex items-center justify-center gap-2 text-center text-sm text-muted">
            {!(approved && complete.isError) && (
              <Spinner size="sm" color="current" aria-hidden="true" />
            )}
            {approved ? (
              <Trans id="pair.approved">Approved. Signing in…</Trans>
            ) : (
              <WaitingLine left={formatSeconds(secondsLeft)} />
            )}
          </p>
        )
      )}
    </PublicStep>
  );
}

function WaitingLine({ left }: { left: string }) {
  return (
    <Trans id="pair.waiting">
      Waiting for approval… The code expires in {left}.
    </Trans>
  );
}

function timedOut(error: unknown): boolean {
  return error instanceof ApiError && error.code === "sudo_required";
}

/**
 * Signed in. A passkey made now lets this device sign in by itself next
 * time; adding one or not, the page goes on to where the sign-in was headed.
 * If the reader waits long enough for the server to want its identity check
 * before adding one, the step is left for the Security page.
 */
function SignedIn({ redirect }: { redirect: string }) {
  const router = useRouter();
  const passkey = useMutation(setupPasskeyMutationOptions());
  const [leaving, setLeaving] = useState<"passkey" | "skip">();
  const [lapsed, setLapsed] = useState(false);
  const supported = window.isSecureContext && browserSupportsWebAuthn();

  const leave = async (via: "passkey" | "skip") => {
    setLeaving(via);
    try {
      await followRedirect(router, redirect);
    } catch {
      setLeaving(undefined);
    }
  };

  const busy = passkey.isPending || leaving !== undefined;
  const passkeyPending = passkey.isPending || leaving === "passkey";

  return (
    <PublicStep
      titleKey="signed-in"
      title={<Trans id="pair.signed_in.title">This device is signed in</Trans>}
      description={
        <Trans id="pair.signed_in.description">
          Add a passkey to sign in on this device directly next time.
        </Trans>
      }
      actions={
        <PublicStep.Actions>
          {lapsed ? (
            <Button
              fullWidth
              isPending={leaving === "skip"}
              onPress={() => void leave("skip")}
            >
              <Trans id="pair.signed_in.continue">Continue</Trans>
            </Button>
          ) : (
            <>
              <PasskeyButton
                supported={supported}
                isPending={passkeyPending}
                isDisabled={busy && !passkeyPending}
                onPress={() => {
                  passkey.mutateAsync().then(
                    () => void leave("passkey"),
                    (error: unknown) => {
                      if (!isCancellation(error) && timedOut(error)) {
                        setLapsed(true);
                      }
                    },
                  );
                }}
              >
                <Trans id="pair.signed_in.passkey">Add a passkey</Trans>
              </PasskeyButton>
              <Button
                variant="tertiary"
                fullWidth
                isPending={leaving === "skip"}
                isDisabled={busy && leaving !== "skip"}
                onPress={() => void leave("skip")}
              >
                <Trans id="pair.signed_in.skip">Not now</Trans>
              </Button>
            </>
          )}
        </PublicStep.Actions>
      }
    />
  );
}
