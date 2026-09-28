import { Link } from "@heroui/react";
import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import {
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { useRouter } from "@tanstack/react-router";
import { type ReactNode, useEffect, useRef, useState } from "react";
import { ApiError, isCancellation } from "@/api/errors";
import {
  federationFlowPrepareMutationOptions,
  federationFlowVerifyMutationOptions,
} from "@/api/mutations";
import { federationFlowQueryOptions } from "@/api/queries";
import type { FederationFlow } from "@/api/raw-paths";
import { followRedirect } from "@/app/redirect";
import { Button } from "@/components/custom/Button";
import { CopyValue } from "@/components/custom/CopyValue";
import { useInstanceBranding } from "@/components/custom/instance-branding";
import { PublicStep } from "@/components/custom/PublicStep";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";
import { Route } from "@/routes/_public.federation.flow.$flow";

const identityRequired = msg({
  id: "federation_flow.identity.required",
  message: "Enter a VRChat profile address or a user ID beginning with usr_.",
});
const usernameRequired = msg({
  id: "federation_flow.username.required",
  message: "Choose a username for your new account.",
});

function failedWith(error: unknown, code: string): boolean {
  return error instanceof ApiError && error.code === code;
}

/** The user ID at the end of a profile address, or the address itself. */
function profileId(profileUrl: string): string {
  try {
    return (
      new URL(profileUrl).pathname.split("/").filter(Boolean).pop() ??
      profileUrl
    );
  } catch {
    return profileUrl;
  }
}

/**
 * Proving a VRChat profile belongs to the reader: name the profile, put the
 * verification link in its bio, and let the server find it there. The flow's
 * own step decides which half is shown; once it is verified the page says so
 * and goes on to wherever the flow leads.
 *
 * The page moves the flow only through its own writes, each of which returns
 * the flow as it now stands. When the server says the page is out of date —
 * the flow moved on in another tab — the flow is read again and the page
 * draws the step it is really at.
 */
export function FederationFlowPage() {
  const { flow: id } = Route.useParams();
  const { data: flow, refetch } = useSuspenseQuery(
    federationFlowQueryOptions(id),
  );
  const [verified, setVerified] = useState<string>();
  const router = useRouter();
  const [leaving, setLeaving] = useState(false);
  const provider = flow.provider.displayName;

  if (verified !== undefined) {
    return (
      <PublicStep
        title={
          <Trans id="federation_flow.verified.title">Profile verified</Trans>
        }
        description={
          <Trans id="federation_flow.verified.description">
            You can remove the verification link from your VRChat bio now.
          </Trans>
        }
        actions={
          <PublicStep.Actions>
            <Button
              fullWidth
              isPending={leaving}
              onPress={() => {
                setLeaving(true);
                followRedirect(router, verified).catch(() => setLeaving(false));
              }}
            >
              <Trans id="federation_flow.continue">Continue</Trans>
            </Button>
          </PublicStep.Actions>
        }
      />
    );
  }

  const step = {
    title: <FlowTitle intent={flow.intent} provider={provider} />,
    description:
      flow.intent === "enroll" ? (
        <Trans id="federation_flow.description.enroll">
          {provider} only proves the profile is yours; next you'll set up your
          own way to sign in. If you already have an account, link {provider}{" "}
          from Security instead.
        </Trans>
      ) : undefined,
  };

  return flow.step === "identify" ? (
    <IdentifyStep id={id} step={step} onStale={refetch} />
  ) : (
    <ProofStep
      id={id}
      flow={flow}
      step={step}
      onVerified={setVerified}
      onStale={refetch}
    />
  );
}

type StepText = { title: ReactNode; description?: ReactNode };

function FlowTitle({
  intent,
  provider,
}: {
  intent: FederationFlow["intent"];
  provider: string;
}) {
  switch (intent) {
    case "login":
      return (
        <Trans id="federation_flow.title.login">
          Sign in through {provider}
        </Trans>
      );
    case "link":
      return (
        <Trans id="federation_flow.title.link">
          Link your {provider} profile
        </Trans>
      );
    default:
      return (
        <Trans id="federation_flow.title.verify">
          Verify your {provider} profile
        </Trans>
      );
  }
}

/** Numbered instructions, with the numbers a step quieter than the text. */
function Steps({ children }: { children: ReactNode }) {
  return (
    <ol className="flex list-decimal flex-col gap-1 ps-5 text-sm marker:text-muted">
      {children}
    </ol>
  );
}

function IdentifyStep({
  id,
  step,
  onStale,
}: {
  id: string;
  step: StepText;
  onStale: () => Promise<unknown>;
}) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const prepare = useMutation(federationFlowPrepareMutationOptions(id));
  const form = useAppForm({
    defaultValues: { identity: "" },
    onSubmit: async ({ value }) => {
      try {
        const next = await prepare.mutateAsync(value.identity.trim());
        queryClient.setQueryData(federationFlowQueryOptions(id).queryKey, next);
      } catch (error) {
        if (isCancellation(error)) return;
        if (failedWith(error, "vrchat_identity_invalid")) {
          applyServerError(form, error, {
            locations: {},
            codes: { vrchat_identity_invalid: "identity" },
          });
        } else if (failedWith(error, "federation_action_invalid")) {
          await onStale();
        }
      }
    },
  });

  return (
    <form.AppForm>
      <form.Form
        label={t({
          id: "federation_flow.identify.form",
          message: "Your VRChat profile",
        })}
      >
        <PublicStep
          titleKey="identify"
          title={step.title}
          description={step.description}
          actions={
            <PublicStep.Actions>
              <form.SubmitButton fullWidth>
                <Trans id="federation_flow.identify.continue">Continue</Trans>
              </form.SubmitButton>
            </PublicStep.Actions>
          }
        >
          <Steps>
            <li>
              <Trans id="federation_flow.identify.step.open">
                Open{" "}
                <Link
                  className="text-sm"
                  href="https://vrchat.com/home"
                  target="_blank"
                  rel="noreferrer"
                >
                  vrchat.com
                </Link>{" "}
                in your browser and go to your profile page.
              </Trans>
            </li>
            <li>
              <Trans id="federation_flow.identify.step.copy">
                Copy the address from the address bar.
              </Trans>
            </li>
          </Steps>
          <form.AppField
            name="identity"
            validators={{
              onBlur: ({ value }) =>
                value.trim().length > 0 ? undefined : identityRequired,
            }}
          >
            {(field) => (
              <field.FormField
                label={
                  <Trans id="federation_flow.identity">
                    VRChat profile address or user ID
                  </Trans>
                }
                description={
                  <Trans id="federation_flow.identity.description">
                    Don't enter your VRChat password here.
                  </Trans>
                }
                placeholder="https://vrchat.com/home/user/usr_…"
                autoComplete="off"
                autoCapitalize="none"
                spellCheck={false}
                isMonospace
                variant="secondary"
              />
            )}
          </form.AppField>
        </PublicStep>
      </form.Form>
    </form.AppForm>
  );
}

function ProofStep({
  id,
  flow,
  step,
  onVerified,
  onStale,
}: {
  id: string;
  flow: FederationFlow;
  step: StepText;
  onVerified: (redirect: string) => void;
  onStale: () => Promise<unknown>;
}) {
  const { t, i18n } = useLingui();
  const { name: instance } = useInstanceBranding();
  const verify = useMutation(federationFlowVerifyMutationOptions(id));
  const usernamePane = useRef<HTMLDivElement>(null);
  // Set when the server asked for a username: the field may only appear once
  // the flow has been read again, so the focus waits for it.
  const focusUsername = useRef(false);
  const requiresLocalUsername = flow.requiresLocalUsername;
  useEffect(() => {
    if (!focusUsername.current || !requiresLocalUsername) return;
    focusUsername.current = false;
    usernamePane.current?.querySelector("input")?.focus();
  }, [requiresLocalUsername]);
  const form = useAppForm({
    defaultValues: { localUsername: "" },
    onSubmit: async ({ value }) => {
      try {
        const { redirect } = await verify.mutateAsync(
          flow.requiresLocalUsername ? value.localUsername : undefined,
        );
        onVerified(redirect);
      } catch (error) {
        if (isCancellation(error)) return;
        if (failedWith(error, "local_username_required")) {
          focusUsername.current = true;
          await onStale();
          const input = usernamePane.current?.querySelector("input");
          if (input) {
            focusUsername.current = false;
            input.focus();
          }
        } else if (failedWith(error, "federation_action_invalid")) {
          await onStale();
        }
      }
    },
  });

  const until = new Intl.DateTimeFormat(i18n.locale, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(flow.expiresAt));
  const profileUrl = flow.profileUrl;

  return (
    <form.AppForm>
      <form.Form
        label={t({
          id: "federation_flow.proof.form",
          message: "Verify your profile",
        })}
      >
        <PublicStep
          titleKey="proof"
          title={step.title}
          description={step.description}
          actions={
            <PublicStep.Actions>
              <form.SubmitButton fullWidth>
                <Trans id="federation_flow.proof.verify">Verify</Trans>
              </form.SubmitButton>
            </PublicStep.Actions>
          }
        >
          {profileUrl && (
            <div className="flex min-w-0 flex-wrap items-baseline gap-x-2 text-sm">
              <span className="text-muted">
                <Trans id="federation_flow.proof.profile">Profile</Trans>
              </span>
              <span className="min-w-0 font-mono wrap-anywhere">
                {profileId(profileUrl)}
              </span>
              <Link
                className="text-sm"
                href={profileUrl}
                target="_blank"
                rel="noreferrer"
              >
                <Trans id="federation_flow.proof.open_profile">
                  Open profile
                </Trans>
              </Link>
            </div>
          )}
          <CopyValue
            value={flow.proofUrl ?? ""}
            label={
              <Trans id="federation_flow.proof.link">Verification link</Trans>
            }
          />
          <Steps>
            <li>
              <Trans id="federation_flow.proof.step.copy">
                Copy the verification link.
              </Trans>
            </li>
            <li>
              <Trans id="federation_flow.proof.step.bio">
                In VRChat, add it to the links in your bio and save.
              </Trans>
            </li>
            <li>
              <Trans id="federation_flow.proof.step.verify">
                Come back here and press Verify.
              </Trans>
            </li>
          </Steps>
          <p className="text-sm text-muted">
            <Trans id="federation_flow.proof.expires">
              The verification link works until {until}.
            </Trans>
          </p>
          {flow.requiresLocalUsername && (
            <div ref={usernamePane}>
              <form.AppField
                name="localUsername"
                validators={{
                  onBlur: ({ value }) =>
                    value.length > 0 ? undefined : usernameRequired,
                }}
              >
                {(field) => (
                  <field.FormField
                    label={
                      <Trans id="federation_flow.username">
                        Username for your new account
                      </Trans>
                    }
                    description={
                      <Trans id="federation_flow.username.description">
                        This is your username on {instance}.
                      </Trans>
                    }
                    autoComplete="username"
                    autoCapitalize="none"
                    spellCheck={false}
                    variant="secondary"
                  />
                )}
              </form.AppField>
            </div>
          )}
        </PublicStep>
      </form.Form>
    </form.AppForm>
  );
}
