import { Plural, Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ListFilterIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { ApiError, isCancellation } from "@/api/errors";
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
import { RuleJsonTab } from "@/pages/admin/groups/RuleJsonTab";
import { ruleRejected } from "@/pages/admin/groups/rule-labels";
import {
  checkRule,
  nodeIdAtPath,
  parseRule,
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
  const { providers, known } = useRuleProviders();
  // The rule as stored, kept here so the section reads as saved the moment the
  // write answers, before the page's own copy of the group has been refetched.
  const [stored, setStored] = useState(group.rule);
  const [tree, setTree] = useState<RuleGroup>(() => parseRule(group.rule));
  const [tab, setTab] = useState("builder");
  const [serverIssues, setServerIssues] =
    useState<readonly RuleIssue[]>(noIssues);
  const [previewFailed, setPreviewFailed] = useState(false);
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

  useEffect(() => {
    if (!dirty || blocked) {
      setServerIssues(noIssues);
      setPreviewFailed(false);
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
        setServerIssues(noIssues);
        setPreviewFailed(false);
      } catch (error) {
        if (!current || isCancellation(error)) return;
        setMatched(null);
        const issue = issueFromRejection(tree, error);
        setServerIssues(issue === null ? noIssues : [issue]);
        setPreviewFailed(issue === null);
      }
    }, 400);
    return () => {
      current = false;
      clearTimeout(timer);
    };
  }, [blocked, dirty, draftKey, previewRule, tree]);

  const discard = () => {
    failure.clear();
    setServerIssues(noIssues);
    setTree(parseRule(stored));
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
          else setServerIssues([issue]);
        },
      },
    );
  };

  const footer = !dirty ? undefined : issues.length > 0 ? (
    <ConsequenceFooter
      tone="danger"
      consequence={<RuleIssueSummary issues={issues} />}
      actionLabel={<Trans id="admin.group.rule.save">Save rule</Trans>}
      onDiscard={discard}
      pending={update.isPending}
      disabled
    />
  ) : (
    <ConsequenceFooter
      consequence={
        <>
          <Trans id="admin.group.rule.consequence">
            Saving changes who is in this group right away.
          </Trans>
          {matched !== null && (
            <span role="status" className="block text-muted-foreground">
              <Plural
                id="admin.group.rule.matched-edited"
                value={matched}
                one="# account matches the edited rule"
                other="# accounts match the edited rule"
              />
            </span>
          )}
          {previewFailed && (
            <span role="status" className="block text-muted-foreground">
              {t(ruleRejected)}
            </span>
          )}
        </>
      }
      actionLabel={<Trans id="admin.group.rule.save">Save rule</Trans>}
      onDiscard={discard}
      pending={update.isPending}
    />
  );

  return (
    <Tabs value={tab} onValueChange={setTab} className="gap-0">
      <form
        onSubmit={(event) => {
          event.preventDefault();
          if (issues.length === 0) save();
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
          dirty={dirty}
          footer={footer}
        >
          <TabsPanel value="builder" keepMounted>
            <RuleBuilder
              value={tree}
              onChange={setTree}
              issues={issues}
              providers={providers}
              disabled={update.isPending}
            />
          </TabsPanel>
          <TabsPanel value="json" keepMounted>
            <RuleJsonTab
              value={tree}
              onApply={setTree}
              disabled={update.isPending}
            />
          </TabsPanel>
          <InlineFailure error={failure.error} />
        </Section>
      </form>
    </Tabs>
  );
}
