import { Trans, useLingui } from "@lingui/react/macro";
import {
  useMutation,
  useQueryClient,
  useQueryErrorResetBoundary,
} from "@tanstack/react-query";
import {
  type ErrorComponentProps,
  useNavigate,
  useRouter,
  useRouterState,
} from "@tanstack/react-router";
import { createContext, type ReactNode, use, useEffect, useState } from "react";
import { describeRouteFailure, type RouteFailure } from "@/api/errors";
import type { RequestExchange } from "@/api/exchange";
import { logoutMutationOptions } from "@/api/mutations";
import { clearSessionQueries } from "@/api/queries";
import { Button } from "@/components/custom/Button";
import { PublicStage } from "@/components/custom/PublicStage";
import { PublicStep } from "@/components/custom/PublicStep";
import { ReferenceLine } from "@/components/custom/ReferenceLine";
import { RequestDetailsDialog } from "@/components/custom/RequestDetailsDialog";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";
import { Frame, FramePanel } from "@/components/ui/frame";
import { Spinner } from "@/components/ui/spinner";

/** Pending state for public routes: the layout supplies the card, this only centers a spinner inside it. */
export function PublicPending() {
  return (
    <div className="flex justify-center py-16">
      <Spinner className="size-6 text-muted-foreground" />
    </div>
  );
}

/**
 * Which layout a page is drawn in. Each layout provides it around its outlet,
 * so a page's failure is drawn in that layout's shape; outside both, the
 * failure takes the whole window.
 */
export const RouteLayoutContext = createContext<"console" | "public" | null>(
  null,
);

type Leave = { to: "/" | "/login"; label: ReactNode };

const consoleLeave: Leave = {
  to: "/",
  label: <Trans id="route.console_home">Back to console home</Trans>,
};
const publicLeave: Leave = {
  to: "/login",
  label: <Trans id="route.back_to_sign_in">Back to sign-in</Trans>,
};
const appLeave: Leave = {
  to: "/",
  label: <Trans id="route.go_home">Go home</Trans>,
};

/**
 * The default error view, so a page needs no error component of its own: it
 * takes the shape of the layout it is drawn in. The root route and the
 * layouts declare `AppRouteError` themselves, because when they fail there is
 * no layout left to draw in.
 */
export function RouteError(props: ErrorComponentProps) {
  const layout = use(RouteLayoutContext);
  if (layout === "console") return <ConsoleRouteError {...props} />;
  if (layout === "public") return <PublicRouteError {...props} />;
  return <AppRouteError {...props} />;
}

/** A console page that failed, in the page column under the header. */
export function ConsoleRouteError({ error }: ErrorComponentProps) {
  const { i18n } = useLingui();
  const route = useRouteFailure(error);
  return (
    <Frame>
      <FramePanel className="flex flex-col gap-1">
        <div role="alert" className="flex flex-col gap-1">
          <p className="font-semibold text-base">
            <Trans id="route.failed">Unable to load this page</Trans>
          </p>
          <p className="text-muted-foreground text-sm">
            {i18n._(route.failure.message)}
          </p>
        </div>
        <FailureBody route={route} leave={consoleLeave} />
      </FramePanel>
    </Frame>
  );
}

/** A public page that failed, on the layout's card in place of the page. */
export function PublicRouteError({ error }: ErrorComponentProps) {
  return <FailedPage error={error} leave={publicLeave} />;
}

/**
 * A failure with no layout to draw in: the root route or a layout itself. It
 * takes the window, since there is no page column to sit in.
 */
export function AppRouteError({ error }: ErrorComponentProps) {
  const { i18n } = useLingui();
  const route = useRouteFailure(error);
  return (
    <WindowStage>
      <PublicStep
        title={<Trans id="route.failed">Unable to load this page</Trans>}
        description={i18n._(route.failure.message)}
      >
        <FailureBody route={route} leave={appLeave} onStage />
      </PublicStep>
    </WindowStage>
  );
}

/** A console address with no page, in the page column under the header. */
export function ConsoleRouteNotFound() {
  return (
    <Frame>
      <FramePanel className="flex flex-col gap-3">
        <p className="font-semibold text-base">
          <Trans id="route.not-found">Page not found</Trans>
        </p>
        <div>
          <LeaveButton to={consoleLeave.to}>{consoleLeave.label}</LeaveButton>
        </div>
      </FramePanel>
    </Frame>
  );
}

/**
 * A plain stage with no toolbar for a failure outside any layout, kept below
 * the toolbar's height so the card lands where the public pages' does.
 */
function WindowStage({ children }: { children: ReactNode }) {
  return (
    <div className="pt-16">
      <PublicStage frame="plain">{children}</PublicStage>
    </div>
  );
}

export function PublicRouteNotFound() {
  return <MissingPage leave={publicLeave} />;
}

export function AppRouteNotFound() {
  return (
    <WindowStage>
      <MissingPage leave={appLeave} />
    </WindowStage>
  );
}

function FailedPage({ error, leave }: { error: unknown; leave: Leave }) {
  const { i18n } = useLingui();
  const route = useRouteFailure(error);
  return (
    <PublicStep
      title={<Trans id="route.failed">Unable to load this page</Trans>}
    >
      <SurfaceAlert status="danger">
        <SurfaceAlert.Indicator />
        <SurfaceAlert.Content>
          <SurfaceAlert.Title>
            {i18n._(route.failure.message)}
          </SurfaceAlert.Title>
        </SurfaceAlert.Content>
      </SurfaceAlert>
      <FailureBody route={route} leave={leave} onStage />
    </PublicStep>
  );
}

function MissingPage({ leave }: { leave: Leave }) {
  return (
    <PublicStep
      title={<Trans id="route.not-found">Page not found</Trans>}
      description={
        <Trans id="route.not-found.description">
          This address doesn't lead to a page. It may have moved or been
          mistyped.
        </Trans>
      }
    >
      <PublicStep.Actions>
        <LeaveButton to={leave.to} size="lg">
          {leave.label}
        </LeaveButton>
      </PublicStep.Actions>
    </PublicStep>
  );
}

type RouteFailureState = ReturnType<typeof useRouteFailure>;

/**
 * Reads the failure and holds what its buttons do. Resetting the query error
 * boundary on mount and before a retry is what makes a suspense query that
 * failed fetch again rather than rethrow its cached error.
 */
function useRouteFailure(error: unknown) {
  const failure = describeRouteFailure(error);
  const { reset } = useQueryErrorResetBoundary();
  const router = useRouter();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [retrying, setRetrying] = useState(false);
  const [details, setDetails] = useState<RequestExchange | null>(null);

  useEffect(() => {
    reset();
  }, [reset]);

  const signOut = useMutation({
    ...logoutMutationOptions(queryClient),
    onSuccess: async () => {
      await clearSessionQueries(queryClient);
      queryClient.getMutationCache().clear();
      await navigate({ to: "/login" });
    },
  });

  const retry = async () => {
    setRetrying(true);
    try {
      reset();
      await router.invalidate();
    } finally {
      setRetrying(false);
    }
  };

  const signIn = async () => {
    await clearSessionQueries(queryClient);
    await navigate({ to: "/login" });
  };

  return { failure, retrying, retry, signIn, signOut, details, setDetails };
}

/**
 * What a failure leaves under its message: the request's reference, then the
 * one way out and, for a request that failed, Details. The request, status and
 * error code live in the details dialog rather than on the page. `onStage`
 * draws the buttons the way the public pages do.
 */
function FailureBody({
  route,
  leave,
  onStage = false,
}: {
  route: RouteFailureState;
  leave: Leave;
  onStage?: boolean;
}) {
  const { failure, details, setDetails } = route;
  const exchange = failure.exchange;
  const size = onStage ? "lg" : "sm";
  const buttons = (
    <>
      <RecoveryButton route={route} leave={leave} size={size} />
      {exchange && (
        <Button
          onClick={() => setDetails(exchange)}
          variant="ghost"
          size={size}
        >
          <Trans id="notification.details">Details</Trans>
        </Button>
      )}
    </>
  );
  return (
    <>
      <FailureReference facts={failure.facts} />
      {onStage ? (
        <PublicStep.Actions>{buttons}</PublicStep.Actions>
      ) : (
        <div className="mt-3 flex flex-wrap gap-2 empty:hidden">{buttons}</div>
      )}
      <RequestDetailsDialog
        exchange={details}
        onClose={() => setDetails(null)}
      />
    </>
  );
}

function RecoveryButton({
  route,
  leave,
  size,
}: {
  route: RouteFailureState;
  leave: Leave;
  size: "sm" | "lg";
}) {
  switch (route.failure.recovery) {
    case "retry":
      return (
        <Button
          pending={route.retrying}
          onClick={() => void route.retry()}
          variant="destructive"
          size={size}
        >
          <Trans id="route.retry">Try again</Trans>
        </Button>
      );
    case "reload":
      return (
        <Button
          onClick={() => window.location.reload()}
          variant="destructive"
          size={size}
        >
          <Trans id="route.reload">Reload</Trans>
        </Button>
      );
    case "sign-in":
      return (
        <Button
          onClick={() => void route.signIn()}
          variant="outline"
          size={size}
        >
          <Trans id="route.sign_in">Sign in</Trans>
        </Button>
      );
    case "sign-out":
      return (
        <Button
          pending={route.signOut.isPending}
          onClick={() => route.signOut.mutate()}
          variant="outline"
          size={size}
        >
          <Trans id="console.logout">Sign out</Trans>
        </Button>
      );
    case "none":
      return (
        <LeaveButton to={leave.to} size={size}>
          {leave.label}
        </LeaveButton>
      );
  }
}

/**
 * The reference an administrator can look up, as the error landing shows it.
 * A page that broke rather than a request that failed has no reference, so its
 * exception is shown instead: nothing else says what broke.
 */
function FailureReference({ facts }: { facts: RouteFailure["facts"] }) {
  if (facts.requestId) return <ReferenceLine reference={facts.requestId} />;
  if (facts.exception) {
    return (
      <p className="font-mono text-muted-foreground text-sm wrap-anywhere">
        {facts.exception}
      </p>
    );
  }
  return null;
}

/** Leaves for `to`; drawn only when the page is somewhere else. */
function LeaveButton({
  to,
  size = "sm",
  children,
}: {
  to: Leave["to"];
  size?: "sm" | "lg";
  children: ReactNode;
}) {
  const navigate = useNavigate();
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  if (pathname === to) return null;
  return (
    <Button onClick={() => void navigate({ to })} variant="outline" size={size}>
      {children}
    </Button>
  );
}
