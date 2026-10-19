import { Plural, Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ListFilterIcon } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { ApiError, describeError, isCancellation } from "@/api/errors";
import {
  rulePreviewMutationOptions,
  updateGroupMutationOptions,
} from "@/api/mutations";
import type { AppGroupView } from "@/api/raw-admin-paths";
import { withInlineReport } from "@/app/query-client";
import { ConsequenceFooter } from "@/components/custom/ConsequenceFooter";
import {
  InlineFailure,
  useInlineFailure,
} from "@/components/custom/InlineFailure";
import { Section } from "@/components/custom/Section";
import { Tabs, TabsList, TabsPanel, TabsTab } from "@/components/ui/tabs";
import { groupUpdateBody } from "@/pages/admin/groups/group-validation";
import { RuleBuilder } from "@/pages/admin/groups/RuleBuilder";
import { RuleIssueSummary } from "@/pages/admin/groups/RuleIssueSummary";
import { RuleJsonProblem, RuleJsonTab } from "@/pages/admin/groups/RuleJsonTab";
import {
  checkRule,
  nodeIdAtPath,
  parseRule,
  type RuleDocumentProblem,
  type RuleGroup,
  type RuleIssue,
  serializeRule,
} from "@/pages/admin/groups/rule-model";
import { useRuleProviders } from "@/pages/admin/groups/use-rule-providers";

const noIssues: readonly RuleIssue[] = [];

/** The server's refusal of a rule, as the row it names and the reason it gives. */
function issueFromRejection(root: RuleGroup, error: unknown): RuleIssue | null {
  if (!(error instanceof ApiError) || error.code !== "invalid_group_rule") {
    return null;
  }
  const { path, reason } = error.details ?? {};
  return {
    nodeId: typeof path === "string" ? nodeIdAtPath(root, path) : null,
    code: typeof reason === "string" ? reason : "rejected",
  };
}

/** Both sets of problems, without saying one twice. */
function merged(
  client: readonly RuleIssue[],
  server: readonly RuleIssue[],
): RuleIssue[] {
  const all = [...client];
  for (const issue of server) {
    if (!all.some((s) => s.nodeId === issue.nodeId && s.code === issue.code)) {
      all.push(issue);
    }
  }
  return all;
}

/**
 * A rule group's rule: the builder and the JSON document over one draft, a live
 * count of who the draft matches, and a save of its own.
 *
 * The preview runs only while the draft differs from the saved rule, debounced,
 * and only the newest answer counts. A rule the builder already knows is
 * invalid is never sent. Saving sends the stored name, identifier and
 * description with the draft, so edits to the details section that were never
 * saved stay out.
 */
export function GroupRuleSection({ group }: { group: AppGroupView }) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const { providers, known, state: providersState } = useRuleProviders();
  // The rule as stored, kept here so the section reads as saved the moment the
  // write answers, before the page's own copy of the group has been refetched.
  const [stored, setStored] = useState(group.rule);
  const [tree, setTree] = useState<RuleGroup>(() => parseRule(group.rule));
  const [tab, setTab] = useState("builder");
  // Text in the JSON box that was refused: it is an edit the tree does not hold.
  const [jsonProblem, setJsonProblem] = useState<RuleDocumentProblem | null>(
    null,
  );
  // Remounts the JSON box on discard, so it drops the text it was refused for.
  const [jsonKey, setJsonKey] = useState(0);
  const [serverIssues, setServerIssues] =
    useState<readonly RuleIssue[]>(noIssues);
  // A preview that failed for a reason that says nothing about the rule.
  const [previewError, setPreviewError] = useState<unknown>(null);
  // The draft a save was refused for: an answer to an older preview must not
  // clear what the server has just said about it.
  const refusedFor = useRef<string | null>(null);
  const [matched, setMatched] = useState<number | null>(null);
  const preview = useMutation(
    withInlineReport(rulePreviewMutationOptions(), true),
  );
  const update = useMutation(
    withInlineReport(updateGroupMutationOptions(queryClient), true),
  );
  const failure = useInlineFailure();
  const { mutateAsync: previewRule } = preview;

  const draft = serializeRule(tree);
  const draftKey = JSON.stringify(draft);
  const dirty = draftKey !== JSON.stringify(serializeRule(parseRule(stored)));
  const clientIssues = checkRule(tree, known);
  const issues = merged(clientIssues, serverIssues);
  const blocked = clientIssues.length > 0;
  const refused = jsonProblem !== null;

  useEffect(() => {
    // A problem found for an earlier draft may point at a row that is gone.
    if (refusedFor.current !== draftKey) {
      refusedFor.current = null;
      setServerIssues(noIssues);
    }
    setPreviewError(null);
    if (!dirty || blocked) {
      setMatched(null);
      return;
    }
    // Typing in one row starts several debounced requests; only the newest may
    // report, or an older rejection would overwrite a newer answer.
    let current = true;
    const timer = setTimeout(async () => {
      try {
        const page = await previewRule(JSON.parse(draftKey));
        if (!current) return;
        setMatched(page.matchedCount);
        if (refusedFor.current !== draftKey) setServerIssues(noIssues);
        setPreviewError(null);
      } catch (error) {
        if (!current || isCancellation(error)) return;
        setMatched(null);
        const issue = issueFromRejection(tree, error);
        if (issue !== null) setServerIssues([issue]);
        setPreviewError(issue === null ? error : null);
      }
    }, 400);
    return () => {
      current = false;
      clearTimeout(timer);
    };
  }, [blocked, dirty, draftKey, previewRule, tree]);

  const discard = () => {
    failure.clear();
    refusedFor.current = null;
    setServerIssues(noIssues);
    setTree(parseRule(stored));
    setJsonProblem(null);
    setJsonKey((key) => key + 1);
  };

  const save = () => {
    failure.clear();
    update.mutate(
      { groupId: group.id, body: groupUpdateBody(group, { rule: draft }) },
      {
        // The stored rule is the new baseline, whatever shape the server kept it in.
        onSuccess: (saved) => {
          setStored(saved.rule);
          setTree(parseRule(saved.rule));
        },
        onError: (error) => {
          const issue = issueFromRejection(tree, error);
          if (issue === null) failure.report(error);
          else {
            refusedFor.current = draftKey;
            setServerIssues([issue]);
          }
        },
      },
    );
  };

  // One footer in both states, so the status line under the sentence stays
  // mounted and a screen reader hears it change.
  const status =
    issues.length > 0 || refused ? null : matched !== null ? (
      <Plural
        id="admin.group.rule.matched-edited"
        value={matched}
        one="# account matches the edited rule"
        other="# accounts match the edited rule"
      />
    ) : previewError !== null ? (
      t(describeError(previewError))
    ) : null;
  const footer = !(dirty || refused) ? undefined : (
    <ConsequenceFooter
      tone={issues.length > 0 || refused ? "danger" : "warning"}
      consequence={
        <>
          {refused ? (
            <RuleJsonProblem problem={jsonProblem} />
          ) : issues.length > 0 ? (
            <RuleIssueSummary issues={issues} />
          ) : (
            <Trans id="admin.group.rule.consequence">
              Saving changes who is in this group right away.
            </Trans>
          )}
          <span role="status" className="block text-muted-foreground">
            {status}
          </span>
        </>
      }
      actionLabel={<Trans id="admin.group.rule.save">Save rule</Trans>}
      onDiscard={discard}
      pending={update.isPending}
      disabled={issues.length > 0 || refused}
    />
  );

  return (
    <Tabs value={tab} onValueChange={setTab} className="gap-0">
      <form
        onSubmit={(event) => {
          event.preventDefault();
          if (issues.length === 0 && !refused) save();
        }}
      >
        <Section
          id="rule"
          icon={<ListFilterIcon />}
          title={
            <Trans id="admin.group.rule.heading">Who this group matches</Trans>
          }
          description={
            <Trans id="admin.group.rule.description">
              Checked every time someone asks for access, so the group is always
              current.
            </Trans>
          }
          action={
            <TabsList
              aria-label={t({
                id: "admin.group.rule.views",
                message: "Rule editor",
              })}
            >
              <TabsTab value="builder">
                <Trans id="admin.group.rule.view.builder">Builder</Trans>
              </TabsTab>
              <TabsTab value="json">
                <Trans id="admin.group.rule.view.json">JSON</Trans>
              </TabsTab>
            </TabsList>
          }
          dirty={dirty || refused}
          footer={footer}
        >
          <TabsPanel value="builder" keepMounted>
            <RuleBuilder
              value={tree}
              onChange={setTree}
              issues={issues}
              providers={providers}
              providersState={providersState}
              disabled={update.isPending}
            />
          </TabsPanel>
          <TabsPanel value="json" keepMounted>
            <RuleJsonTab
              key={jsonKey}
              value={tree}
              onApply={setTree}
              onProblem={setJsonProblem}
              disabled={update.isPending}
            />
          </TabsPanel>
          <InlineFailure error={failure.error} />
        </Section>
      </form>
    </Tabs>
  );
}
