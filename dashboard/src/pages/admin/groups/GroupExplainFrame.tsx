import { Trans, useLingui } from "@lingui/react/macro";
import { useQuery } from "@tanstack/react-query";
import { CircleCheckIcon, CircleXIcon, ScanSearchIcon } from "lucide-react";
import { useState } from "react";
import {
  accountSearchQueryOptions,
  groupExplainQueryOptions,
  groupProvidersQueryOptions,
} from "@/api/queries";
import type { ExplanationView } from "@/api/raw-admin-paths";
import { quietQuery } from "@/app/query-client";
import {
  type EntityOption,
  EntityPicker,
} from "@/components/custom/EntityPicker";
import { InlineFailure } from "@/components/custom/InlineFailure";
import { Section } from "@/components/custom/Section";
import { Spinner } from "@/components/ui/spinner";
import {
  type ExplainSentence,
  explainSentence,
} from "@/pages/admin/groups/explain-labels";

const heading = {
  id: "check-account",
  title: <Trans id="admin.group.explain.title">Check an account</Trans>,
  description: (
    <Trans id="admin.group.explain.description">Why someone is in or out</Trans>
  ),
  icon: <ScanSearchIcon />,
};

/**
 * Picks one account and shows, condition by condition, why the saved rule does
 * or does not include it. The server answers with a tree of results and never
 * with the account's own facts.
 */
export function GroupExplainFrame({ groupId }: { groupId: number }) {
  const { t } = useLingui();
  const [search, setSearch] = useState("");
  // The directory is read when the reader opens the picker or types, not when
  // the page opens.
  const [searching, setSearching] = useState(false);
  const [chosen, setChosen] = useState<EntityOption | null>(null);
  const candidates = useQuery({
    ...accountSearchQueryOptions(search),
    enabled: searching,
  });
  // Disabled accounts stay listed with the reason: the server explains nothing
  // for them, and an administrator looking for one learns why.
  const options: EntityOption[] = (candidates.data?.items ?? []).map(
    (account) => ({
      id: String(account.id),
      label: account.displayName || account.username,
      ...(account.displayName ? { identifier: `@${account.username}` } : {}),
      ...(account.disabled
        ? {
            unavailableReason: t({
              id: "app.managers.candidate.disabled",
              message: "Disabled account",
            }),
          }
        : {}),
    }),
  );
  // The chosen account keeps its name when a later search no longer lists it.
  const listed =
    chosen === null || options.some((option) => option.id === chosen.id)
      ? options
      : [chosen, ...options];

  return (
    <Section {...heading}>
      <EntityPicker
        label={<Trans id="admin.group.explain.account">Account</Trans>}
        placeholder={t({
          id: "admin.group.explain.placeholder",
          message: "Search accounts",
        })}
        searchPlaceholder={t({
          id: "admin.group.explain.placeholder",
          message: "Search accounts",
        })}
        searchLabel={t({ id: "admin.picker.search", message: "Search" })}
        options={listed}
        loading={candidates.isFetching}
        onSearch={(query) => {
          setSearching(true);
          setSearch(query);
        }}
        onOpenChange={(open) => open && setSearching(true)}
        value={chosen === null ? [] : [chosen.id]}
        onValueChange={([id]) =>
          setChosen(listed.find((option) => option.id === id) ?? null)
        }
      />
      {chosen !== null && (
        <Explanation
          key={chosen.id}
          groupId={groupId}
          accountId={Number(chosen.id)}
        />
      )}
    </Section>
  );
}

function Explanation({
  groupId,
  accountId,
}: {
  groupId: number;
  accountId: number;
}) {
  const { t } = useLingui();
  const explanation = useQuery(
    quietQuery(groupExplainQueryOptions(groupId, accountId)),
  );
  const providers = useQuery(quietQuery(groupProvidersQueryOptions()));
  const names = new Map(
    (providers.data ?? []).map((provider) => [
      provider.slug,
      provider.displayName,
    ]),
  );
  const providerName = (slug: string) => names.get(slug);

  if (explanation.isError) return <InlineFailure error={explanation.error} />;
  // The tree waits for the provider names too, so a name never replaces a slug
  // after it is drawn.
  if (explanation.data === undefined || providers.isPending) {
    return (
      <p className="flex items-center gap-2 text-muted-foreground text-sm">
        <Spinner aria-hidden="true" className="size-4" />
        <Trans id="admin.group.explain.checking">Checking this account</Trans>
      </p>
    );
  }
  const root = explanation.data.explanation;

  return (
    <div className="flex flex-col gap-3">
      {root.result ? (
        <p
          role="status"
          className="flex items-center gap-2 font-medium text-sm text-success-foreground"
        >
          <CircleCheckIcon aria-hidden="true" className="size-4" />
          <Trans id="admin.group.explain.in">In this group</Trans>
        </p>
      ) : (
        <p
          role="status"
          className="flex items-center gap-2 font-medium text-destructive-foreground text-sm"
        >
          <CircleXIcon aria-hidden="true" className="size-4" />
          <Trans id="admin.group.explain.out">Not in this group</Trans>
        </p>
      )}
      <ul
        aria-label={t({
          id: "admin.group.explain.conditions",
          message: "Conditions",
        })}
        className="flex flex-col gap-1.5 text-sm"
      >
        <ExplainNode node={root} providerName={providerName} />
      </ul>
    </div>
  );
}

/** A sentence, with a provider the console cannot name drawn as an identifier. */
function SentenceText({ sentence }: { sentence: ExplainSentence }) {
  if ("text" in sentence) return sentence.text;
  if ("provider" in sentence) {
    // One unit: a name moves to the next line whole rather than splitting.
    const { provider } = sentence;
    return (
      <Trans id="admin.group.explain.provider">
        Is connected to{" "}
        <span
          data-slot="explain-provider"
          className="inline-block max-w-full wrap-anywhere"
        >
          {provider}
        </span>
      </Trans>
    );
  }
  const provider = sentence.unknownProvider;
  return (
    <Trans id="admin.group.explain.provider-unknown">
      Is connected to{" "}
      <span data-literal="" className="break-all font-mono text-xs">
        {provider}
      </span>
    </Trans>
  );
}

/** One node and, nested a level in, the nodes it was decided from. */
function ExplainNode({
  node,
  providerName,
}: {
  node: ExplanationView;
  providerName: (slug: string) => string | undefined;
}) {
  const { t } = useLingui();
  const sentence = explainSentence(node.label, t, providerName);
  const children = node.children ?? [];
  return (
    <li className="flex flex-col gap-1.5">
      <span className="flex items-start gap-2">
        {node.result ? (
          <CircleCheckIcon
            aria-hidden="true"
            className="mt-0.5 size-4 shrink-0 text-success"
          />
        ) : (
          <CircleXIcon
            aria-hidden="true"
            className="mt-0.5 size-4 shrink-0 text-destructive"
          />
        )}
        <span className="sr-only">
          {node.result ? (
            <Trans id="admin.group.explain.applies">Applies:</Trans>
          ) : (
            <Trans id="admin.group.explain.not-applies">Does not apply:</Trans>
          )}
        </span>
        {sentence !== undefined ? (
          <span className="min-w-0 text-pretty">
            <SentenceText sentence={sentence} />
          </span>
        ) : (
          <span
            data-raw-label=""
            className="min-w-0 break-all font-mono text-xs"
          >
            {node.label}
          </span>
        )}
      </span>
      {children.length > 0 && (
        <ul className="ms-2 flex flex-col gap-1.5 border-s ps-4">
          {children.map((child) => (
            <ExplainNode
              key={child.path}
              node={child}
              providerName={providerName}
            />
          ))}
        </ul>
      )}
    </li>
  );
}
