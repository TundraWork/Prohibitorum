import {
  CircleAlertIcon,
  CircleCheckIcon,
  InfoIcon,
  TriangleAlertIcon,
} from "lucide-react";
import {
  type ComponentProps,
  createContext,
  type ReactNode,
  useContext,
} from "react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";

type Status = "default" | "accent" | "success" | "warning" | "danger";

const variants = {
  default: "default",
  accent: "info",
  success: "success",
  warning: "warning",
  danger: "error",
} as const;

const icons = {
  default: InfoIcon,
  accent: InfoIcon,
  success: CircleCheckIcon,
  warning: TriangleAlertIcon,
  danger: CircleAlertIcon,
};

const StatusContext = createContext<Status>("default");

/**
 * The console's notice, on coss's `Alert`, keeping the status vocabulary and
 * the Indicator / Content / Title / Description parts screens already compose.
 * `Content` adds no element of its own, so the title and description stay
 * direct children of the alert and take its grid.
 */
function SurfaceAlertRoot({
  status = "default",
  role,
  ...props
}: Omit<ComponentProps<typeof Alert>, "variant"> & { status?: Status }) {
  return (
    <StatusContext.Provider value={status}>
      {/* Announces only when the screen asks it to, as a static notice should. */}
      <Alert variant={variants[status]} role={role} {...props} />
    </StatusContext.Provider>
  );
}

function Indicator() {
  const Icon = icons[useContext(StatusContext)];
  return <Icon aria-hidden="true" />;
}

function Content({ children }: { children?: ReactNode }) {
  return <>{children}</>;
}

export const SurfaceAlert = Object.assign(SurfaceAlertRoot, {
  Indicator,
  Content,
  Title: AlertTitle,
  Description: AlertDescription,
});
