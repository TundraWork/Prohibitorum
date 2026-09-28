/** The sign-in steps, where the administrators' way in is honoured. */
const signInPaths = new Set(["/login", "/login/totp", "/login/recovery"]);

/**
 * Whether a navigation goes to the maintenance page instead. During
 * maintenance only an administrator uses the instance; everyone else, signed
 * in or not, is shown the maintenance page. Two addresses stay open: that page
 * itself, and a sign-in step whose search names `admin`, whatever its value,
 * which is how an administrator gets in.
 */
export function maintenanceRedirect({
  maintenanceMode,
  session,
  pathname,
  search,
}: {
  maintenanceMode: boolean;
  session: { role: string } | null;
  pathname: string;
  search: Record<string, unknown>;
}): boolean {
  if (!maintenanceMode) return false;
  if (session?.role === "admin") return false;
  if (pathname === "/maintenance") return false;
  if (signInPaths.has(pathname) && Object.hasOwn(search, "admin")) return false;
  return true;
}
