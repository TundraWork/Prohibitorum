import {
  Button,
  Description,
  FieldError,
  Input,
  Label,
  TextField,
} from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { describeError } from "@/api/errors";
import {
  approveDeviceMutationOptions,
  cancelDeviceMutationOptions,
  deviceLookupQueryOptions,
} from "@/api/mutations";

import { ConsoleCard } from "@/components/custom/ConsoleCard";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";

/**
 * The already-signed-in half of device pairing. The other device shows a code
 * and polls for approval; this page looks the code up, shows who is asking, and
 * lets the account approve or decline.
 *
 * The code is passed through exactly as typed: the server owns the format, and
 * normalising it here would only hide a real mismatch. Only an empty box is
 * rejected locally.
 */
export function Devices() {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const [code, setCode] = useState("");
  const [submitted, setSubmitted] = useState<string | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [outcome, setOutcome] = useState<"approved" | "cancelled" | null>(null);
  const [inputError, setInputError] = useState<string | null>(null);

  const lookup = useQuery({
    ...deviceLookupQueryOptions(submitted ?? ""),
    enabled: submitted !== null,
  });

  const approve = useMutation(approveDeviceMutationOptions(queryClient));
  const decline = useMutation(cancelDeviceMutationOptions(queryClient));

  const busy = approve.isPending || decline.isPending;
  const found = lookup.data;

  const formatTime = (value: string) =>
    new Intl.DateTimeFormat(undefined, {
      dateStyle: "medium",
      timeStyle: "short",
    }).format(new Date(value));

  return (
    <>
      <p className="text-sm text-muted">
        <Trans id="devices.intro">
          Signing in somewhere new? Enter the code that device is showing, check
          it is really you, and approve it here.
        </Trans>
      </p>

      <ConsoleCard
        title={<Trans id="devices.code.title">Pairing code</Trans>}
        contentClassName="flex flex-col gap-4"
      >
        <form
          className="flex flex-col gap-4"
          aria-label={t({ id: "devices.form", message: "Device pairing" })}
          onSubmit={(event) => {
            event.preventDefault();
            setError(null);
            setOutcome(null);
            setInputError(null);
            if (code.trim() === "") {
              setInputError(
                t({
                  id: "devices.code.required",
                  message: "Enter the pairing code shown on the other device.",
                }),
              );
              return;
            }
            if (submitted === code) {
              // Same code, so the query would serve its cache; ask again.
              void lookup.refetch();
              return;
            }
            setSubmitted(code);
          }}
        >
          <TextField
            name="code"
            value={code}
            isInvalid={inputError !== null}
            validationBehavior="aria"
            onChange={(value) => {
              setInputError(null);
              setCode(value);
            }}
          >
            <Label>
              <Trans id="devices.code.label">Code from the other device</Trans>
            </Label>
            <Input
              autoComplete="one-time-code"
              spellCheck={false}
              autoCapitalize="characters"
              className="font-mono"
              aria-invalid={inputError !== null || undefined}
              variant="secondary"
            />
            <Description>
              <Trans id="devices.code.hint">
                Pairing codes are short-lived. If yours has expired, generate a
                new one on that device.
              </Trans>
            </Description>
            {inputError && <FieldError>{inputError}</FieldError>}
          </TextField>
          <div>
            <Button type="submit" isPending={lookup.isFetching}>
              <Trans id="devices.lookup">Look up code</Trans>
            </Button>
          </div>
        </form>

        {lookup.isError && lookup.error !== null && (
          <SurfaceAlert status="danger" role="alert">
            <SurfaceAlert.Indicator />
            <SurfaceAlert.Content>
              <SurfaceAlert.Title>
                {t(describeError(lookup.error))}
              </SurfaceAlert.Title>
            </SurfaceAlert.Content>
          </SurfaceAlert>
        )}

        {error !== null && (
          <SurfaceAlert status="danger" role="alert">
            <SurfaceAlert.Indicator />
            <SurfaceAlert.Content>
              <SurfaceAlert.Title>{t(describeError(error))}</SurfaceAlert.Title>
            </SurfaceAlert.Content>
          </SurfaceAlert>
        )}

        {outcome === "approved" && (
          <SurfaceAlert status="success" role="status">
            <SurfaceAlert.Indicator />
            <SurfaceAlert.Content>
              <SurfaceAlert.Title>
                <Trans id="devices.approved">
                  You approved this device. It still has to set up a passkey on
                  itself before the sign-in finishes.
                </Trans>
              </SurfaceAlert.Title>
            </SurfaceAlert.Content>
          </SurfaceAlert>
        )}

        {outcome === "cancelled" && (
          <SurfaceAlert status="warning" role="status">
            <SurfaceAlert.Indicator />
            <SurfaceAlert.Content>
              <SurfaceAlert.Title>
                <Trans id="devices.cancelled">
                  You declined this pairing. The other device does not get
                  access.
                </Trans>
              </SurfaceAlert.Title>
            </SurfaceAlert.Content>
          </SurfaceAlert>
        )}

        {found && outcome === null && (
          <div className="flex flex-col gap-4">
            <h3 className="text-lg font-semibold">
              <Trans id="devices.confirm.title">
                Check this is the device you started
              </Trans>
            </h3>
            <dl className="grid min-w-0 gap-4 sm:grid-cols-2">
              <div className="min-w-0">
                <dt className="text-sm text-muted">
                  <Trans id="devices.field.code">Code</Trans>
                </dt>
                <dd className="wrap-anywhere font-mono">{found.displayCode}</dd>
              </div>
              <div className="min-w-0">
                <dt className="text-sm text-muted">
                  <Trans id="devices.field.ip">Requested from</Trans>
                </dt>
                <dd className="wrap-anywhere">{found.initiatorIp || "—"}</dd>
              </div>
              <div className="min-w-0">
                <dt className="text-sm text-muted">
                  <Trans id="devices.field.agent">Device</Trans>
                </dt>
                <dd className="wrap-anywhere">{found.initiatorUa || "—"}</dd>
              </div>
              <div className="min-w-0">
                <dt className="text-sm text-muted">
                  <Trans id="devices.field.created">Requested</Trans>
                </dt>
                <dd className="wrap-anywhere">{formatTime(found.createdAt)}</dd>
              </div>
              <div className="min-w-0">
                <dt className="text-sm text-muted">
                  <Trans id="devices.field.expires">Expires</Trans>
                </dt>
                <dd className="wrap-anywhere">{formatTime(found.expiresAt)}</dd>
              </div>
            </dl>
            {found.alreadyBound && (
              <SurfaceAlert status="warning">
                <SurfaceAlert.Indicator />
                <SurfaceAlert.Content>
                  <SurfaceAlert.Title>
                    <Trans id="devices.already_bound">
                      You have already approved this pairing. The other device
                      should be finishing its setup.
                    </Trans>
                  </SurfaceAlert.Title>
                </SurfaceAlert.Content>
              </SurfaceAlert>
            )}
            <div className="flex flex-wrap gap-2">
              <Button
                isDisabled={busy || found.alreadyBound}
                isPending={approve.isPending}
                onPress={() => {
                  setError(null);
                  approve.mutate(submitted as string, {
                    onSuccess: () => setOutcome("approved"),
                    onError: (failure) => setError(failure),
                  });
                }}
              >
                <Trans id="devices.approve">Approve this device</Trans>
              </Button>
              <Button
                variant="secondary"
                isDisabled={busy}
                isPending={decline.isPending}
                onPress={() => {
                  setError(null);
                  decline.mutate(submitted as string, {
                    onSuccess: () => setOutcome("cancelled"),
                    onError: (failure) => setError(failure),
                  });
                }}
              >
                <Trans id="devices.decline">Decline</Trans>
              </Button>
            </div>
            <Description>
              <Trans id="devices.note">
                Approving lets this device continue, but it is not signed in
                yet: it still has to set up a passkey of its own.
              </Trans>
            </Description>
          </div>
        )}
      </ConsoleCard>
    </>
  );
}
