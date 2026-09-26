import { createFileRoute } from "@tanstack/react-router";
import { identityProviderQueryOptions } from "@/api/queries";
import { AdminIdentityProvider } from "@/pages/admin/identity-providers/AdminIdentityProvider";
import { readRunId } from "@/pages/admin/identity-providers/ProviderDiagnosticsSection";

/**
 * One provider's settings, addressed by slug.
 *
 * The slug is the identifier the server uses everywhere the provider is named —
 * the callback path, the icon URL, the auth routes — so it is the route segment
 * rather than something the console assigns. The loader reads the provider
 * because every section draws from it; each section then reads whatever else it
 * needs for itself.
 *
 * `test` is where the OIDC test callback lands: the server redirects the browser
 * to `/admin/identity-providers/{slug}?test={id}`, so the run id arrives as a
 * search value. It is validated here rather than in the page so a hand-edited URL
 * cannot put an arbitrary string in a request path, and it is the page's own
 * state once read — closing a result drops it from the URL.
 */
export const Route = createFileRoute(
  "/_protected/admin/_admin/identity-providers/$slug",
)({
  validateSearch: (search: Record<string, unknown>) => {
    const test = readRunId(search.test);
    // The key is omitted rather than set to undefined when there is no run, so a
    // link to this page does not have to name a search value that does not exist.
    return test === undefined ? {} : { test };
  },
  loader: ({ context: { queryClient }, params: { slug } }) =>
    queryClient.ensureQueryData(identityProviderQueryOptions(slug)),
  component: AdminIdentityProviderPage,
});

function AdminIdentityProviderPage() {
  const { slug } = Route.useParams();
  const { test } = Route.useSearch();
  const navigate = Route.useNavigate();

  return (
    <AdminIdentityProvider
      slug={slug}
      runId={test}
      onRunIdChange={(runId) => {
        // `replace` so closing a result does not add a history entry the reader
        // has to step back through to leave the page.
        void navigate({
          search: { test: runId },
          replace: true,
        });
      }}
    />
  );
}
