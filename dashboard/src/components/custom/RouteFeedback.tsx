import { Alert, Spinner } from "@heroui/react";
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
import {
  createContext,
  Fragment,
  type ReactNode,
  use,
  useCallback,
  useEffect,
  useState,
} from "react";
import { describeRouteFailure, type RouteFailure } from "@/api/errors";
import type { RequestExchange } from "@/api/exchange";
import { logoutMutationOptions } from "@/api/mutations";
import { clearSessionQueries } from "@/api/queries";
import { Button } from "@/components/custom/Button";
import { PublicCard } from "@/components/custom/PublicCard";
import { RequestDetailsDialog } from "@/components/custom/RequestDetailsDialog";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";

/** Pending state for public routes: the layout supplies the card, this only centers a spinner inside it. */
export function PublicPending() {
  return (
    <div className="flex justify-center py-16">
      <Spinner size="lg" />
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

/** A console page that failed, in the page column under the header's title. */
export function ConsoleRouteError({ error }: ErrorComponentProps) {
  const { i18n } = useLingui();
  const route = useRouteFailure(error);
  return (
    <Alert status="danger">
      <Alert.Indicator />
      <Alert.Content>
        <Alert.Title role="alert">{i18n._(route.failure.message)}</Alert.Title>
        <FailureBody route={route} leave={consoleLeave} />
      </Alert.Content>
    </Alert>
  );
}

/** A public page that failed, on the layout's card in place of the page. */
export function PublicRouteError({ error }: ErrorComponentProps) {
  return <FailedPage error={error} leave={publicLeave} />;
}

/** A failure with no layout to draw in: the root route or a layout itself. */
export function AppRouteError({ error }: ErrorComponentProps) {
  return (
    <PublicCard>
      <FailedPage error={error} leave={appLeave} />
    </PublicCard>
  );
}

/** The console header already says the page does not exist. */
export function ConsoleRouteNotFound() {
  return (
    <div>
      <LeaveButton to={consoleLeave.to}>{consoleLeave.label}</LeaveButton>
    </div>
  );
}

export function PublicRouteNotFound() {
  return <MissingPage leave={publicLeave} />;
}

export function AppRouteNotFound() {
  return (
    <PublicCard>
      <MissingPage leave={appLeave} />
    </PublicCard>
  );
}

function FailedPage({ error, leave }: { error: unknown; leave: Leave }) {
  const { i18n } = useLingui();
  const route = useRouteFailure(error);
  return (
    <div className="flex flex-col gap-4">
      <PageHeading>
        <Trans id="route.failed">Unable to load this page</Trans>
      </PageHeading>
      <SurfaceAlert status="danger">
        <SurfaceAlert.Indicator />
        <SurfaceAlert.Content>
          <SurfaceAlert.Title>
            {i18n._(route.failure.message)}
          </SurfaceAlert.Title>
          <FailureBody route={route} leave={leave} />
        </SurfaceAlert.Content>
      </SurfaceAlert>
    </div>
  );
}

function MissingPage({ leave }: { leave: Leave }) {
  return (
    <div className="flex flex-col gap-4">
      <PageHeading>
        <Trans id="route.not-found">Page not found</Trans>
      </PageHeading>
      <div>
        <LeaveButton to={leave.to}>{leave.label}</LeaveButton>
      </div>
    </div>
  );
}

/**
 * The page's title in place of the one it replaced. It takes focus as it
 * appears, so a screen reader starts from the new page; it is not a control,
 * so it draws no focus ring.
 */
function PageHeading({ children }: { children: ReactNode }) {
  const focus = useCallback((node: HTMLHeadingElement | null) => {
    node?.focus();
  }, []);
  return (
    <h1
      ref={focus}
      tabIndex={-1}
      className="min-w-0 text-xl font-semibold wrap-anywhere outline-none"
    >
      {children}
    </h1>
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

/** The facts under the failure's title, then its buttons. */
function FailureBody({
  route,
  leave,
}: {
  route: RouteFailureState;
  leave: Leave;
}) {
  const { failure, details, setDetails } = route;
  const exchange = failure.exchange;
  return (
    <>
      <RouteFacts facts={failure.facts} />
      <div className="mt-3 flex flex-wrap gap-2 empty:hidden">
        <RecoveryButton route={route} leave={leave} />
        {exchange && (
          <Button
            size="sm"
            variant="tertiary"
            onPress={() => setDetails(exchange)}
          >
            <Trans id="notification.details">Details</Trans>
          </Button>
        )}
      </div>
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
}: {
  route: RouteFailureState;
  leave: Leave;
}) {
  switch (route.failure.recovery) {
    case "retry":
      return (
        <Button
          size="sm"
          variant="danger"
          isPending={route.retrying}
          onPress={() => void route.retry()}
        >
          <Trans id="route.retry">Try again</Trans>
        </Button>
      );
    case "reload":
      return (
        <Button
          size="sm"
          variant="danger"
          onPress={() => window.location.reload()}
        >
          <Trans id="route.reload">Reload</Trans>
        </Button>
      );
    case "sign-in":
      return (
        <Button
          size="sm"
          variant="secondary"
          onPress={() => void route.signIn()}
        >
          <Trans id="route.sign_in">Sign in</Trans>
        </Button>
      );
    case "sign-out":
      return (
        <Button
          size="sm"
          variant="secondary"
          isPending={route.signOut.isPending}
          onPress={() => route.signOut.mutate()}
        >
          <Trans id="console.logout">Sign out</Trans>
        </Button>
      );
    case "none":
      return <LeaveButton to={leave.to}>{leave.label}</LeaveButton>;
  }
}

/**
 * The failure's facts, one per row: label beside value from `sm`, stacked
 * below it. A fact the failure does not have is left out.
 */
function RouteFacts({ facts }: { facts: RouteFailure["facts"] }) {
  const rows: {
    key: keyof RouteFailure["facts"];
    label: ReactNode;
    mono: boolean;
  }[] = [
    {
      key: "request",
      label: <Trans id="request_details.request">Request</Trans>,
      mono: true,
    },
    {
      key: "status",
      label: <Trans id="request_details.status">Status</Trans>,
      mono: false,
    },
    {
      key: "code",
      label: <Trans id="route.error.code">Error code</Trans>,
      mono: true,
    },
    {
      key: "requestId",
      label: <Trans id="route.error.request_id">Request ID</Trans>,
      mono: true,
    },
    {
      key: "exception",
      label: <Trans id="route.error.exception">Exception</Trans>,
      mono: true,
    },
  ];
  const shown = rows.filter(
    ({ key }) => facts[key] !== undefined && facts[key] !== "",
  );
  if (shown.length === 0) return null;
  return (
    <dl className="mt-2 grid gap-x-4 gap-y-1 text-sm sm:grid-cols-[max-content_minmax(0,1fr)]">
      {shown.map(({ key, label, mono }) => (
        <Fragment key={key}>
          <dt className="text-muted">{label}</dt>
          <dd
            className={`min-w-0 wrap-anywhere text-foreground max-sm:not-last:mb-1 ${mono ? "font-mono" : ""}`}
          >
            {facts[key]}
          </dd>
        </Fragment>
      ))}
    </dl>
  );
}

/** Leaves for `to`; drawn only when the page is somewhere else. */
function LeaveButton({
  to,
  children,
}: {
  to: Leave["to"];
  children: ReactNode;
}) {
  const navigate = useNavigate();
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  if (pathname === to) return null;
  return (
    <Button size="sm" variant="secondary" onPress={() => void navigate({ to })}>
      {children}
    </Button>
  );
}
