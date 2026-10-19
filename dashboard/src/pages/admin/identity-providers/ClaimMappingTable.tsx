import { Trans, useLingui } from "@lingui/react/macro";
import { createContext, type ReactNode, useContext, useId } from "react";
import type {
  LatestConnectionTestFieldView,
  LatestConnectionTestView,
} from "@/api/raw-admin-paths";
import { FormMessages } from "@/components/custom/FormMessages";
import { RelativeTime } from "@/components/custom/RelativeTime";
import { useFieldBinding } from "@/components/custom/use-field-binding";
import { Field, FieldError } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";
import { documentSourceLabel } from "@/pages/admin/identity-providers/provider-options";

/**
 * What the latest connection test saw of each field: absent while no finished
 * test says anything usable. A failed test stopped before it read an identity,
 * so it has no fields to report and is told apart from one that read them.
 */
type LastTest = LatestConnectionTestView | null | undefined;

/** The per-field results of a test that read an identity, or none to show. */
function returnedFields(
  test: LastTest,
): Record<string, LatestConnectionTestFieldView> | null {
  return test?.status === "succeeded" ? test.fields : null;
}

const ReturnedFieldsContext = createContext<Record<
  string,
  LatestConnectionTestFieldView
> | null>(null);

/**
 * The tracks of the wide table. With the last-test column it takes three, and
 * so needs more room before it leaves the stacked layout; the two-column table
 * keeps the width it always had. Written out in full so the class scanner sees
 * every variant.
 */
const layouts = {
  plain: {
    head: "hidden @lg/claims:grid @lg/claims:grid-cols-[10rem_minmax(0,1fr)] @lg/claims:gap-x-6",
    row: "@lg/claims:grid @lg/claims:grid-cols-[10rem_minmax(0,1fr)] @lg/claims:items-start @lg/claims:gap-x-6",
    // Where the first two heading cells appear: with the head itself.
    names: "",
  },
  tested: {
    head: "grid @2xl/claims:grid-cols-[10rem_minmax(0,1fr)_minmax(0,1fr)] @2xl/claims:gap-x-6",
    row: "@2xl/claims:grid @2xl/claims:grid-cols-[10rem_minmax(0,1fr)_minmax(0,1fr)] @2xl/claims:items-start @2xl/claims:gap-x-6",
    names: "hidden @2xl/claims:block",
  },
} as const;

/**
 * Which provider claim fills each account field: the account's field, the claim
 * name typed beside it, and, when a connection test has run, what that test
 * read for the field.
 *
 * The five rows are fixed — they are the fields an account has — so the table
 * never adds or removes one, and each row is its own form field. It switches on
 * its own width rather than the viewport's, since the panel is what has or
 * lacks the room: wide, the names, inputs and results line up under column
 * headings; narrow, each name sits over its input and its result, and the
 * headings go. The last-test column's own heading stays at every width because
 * it says which test the results are from and whether the settings have moved
 * since.
 *
 * A test that failed read no fields, so it has no column: one line says it
 * failed, rather than every row saying it was missing. A provider nobody has
 * tested has no column either.
 */
export function ClaimMappingTable({
  lastTest,
  children,
}: {
  lastTest: LastTest;
  children: ReactNode;
}) {
  const labelId = useId();
  const captionId = useId();
  const fields = returnedFields(lastTest);
  const layout = fields === null ? layouts.plain : layouts.tested;

  return (
    <div className="@container/claims flex flex-col gap-2">
      <div id={labelId} className="font-medium text-base/4.5 sm:text-sm/4">
        <Trans id="admin.federation.claims.mapping">Claim mapping</Trans>
      </div>
      {lastTest?.status === "failed" && (
        <p
          id={captionId}
          data-slot="claims-last-test-failed"
          className="text-warning-foreground text-xs"
        >
          <Trans id="admin.federation.claims.test.failed">
            The last test failed <RelativeTime value={lastTest.finishedAt} />.
            Run it again from Diagnostics.
          </Trans>
          {lastTest.stale && (
            <>
              {" "}
              <Trans id="admin.federation.claims.column.returned.stale">
                Configuration changed since
              </Trans>
              .
            </>
          )}
        </p>
      )}
      <div
        className={cn(
          "gap-y-0.5 text-muted-foreground text-xs font-medium",
          layout.head,
        )}
      >
        <span aria-hidden="true" className={layout.names}>
          <Trans id="admin.federation.claims.column.field">Account field</Trans>
        </span>
        <span aria-hidden="true" className={layout.names}>
          <Trans id="admin.federation.claims.column.claim">
            Provider claim
          </Trans>
        </span>
        {fields !== null && lastTest != null && (
          <span className="flex flex-col">
            <Trans id="admin.federation.claims.column.returned">
              Last test returned
            </Trans>
            <span id={captionId} className="flex flex-col font-normal">
              <span>
                <Trans id="admin.federation.claims.column.returned.when">
                  From the test <RelativeTime value={lastTest.finishedAt} />
                </Trans>
              </span>
              {lastTest.stale && (
                <span className="text-warning-foreground">
                  <Trans id="admin.federation.claims.column.returned.stale">
                    Configuration changed since
                  </Trans>
                </span>
              )}
            </span>
          </span>
        )}
      </div>
      <ReturnedFieldsContext.Provider value={fields}>
        <dl
          aria-labelledby={labelId}
          aria-describedby={lastTest == null ? undefined : captionId}
          className="flex flex-col divide-y divide-border"
        >
          {children}
        </dl>
      </ReturnedFieldsContext.Provider>
      <p className="text-muted-foreground text-xs">
        <Trans id="admin.federation.claims.mapping.path-hint">
          A name with dots is read as the exact claim key first; only when no
          claim is spelled that way is it read as a path into nested claims,
          like avatar.large or emails.0.email.
        </Trans>
      </p>
    </div>
  );
}

/**
 * One account field and the claim it is read from. The claim name is monospace
 * because it is the literal key in the provider's token. The input is named by
 * the account field's own text rather than a second label, so a reader hears
 * the name once.
 */
export function ClaimMappingRow({
  name,
  hint,
  placeholder,
  testField,
}: {
  /** The account field. */
  name: string;
  /** A line under the name, for the row whose value has a consequence. */
  hint?: ReactNode;
  /** The server's default claim for this field. */
  placeholder: string;
  /** The key the last test reports this field under. */
  testField: string;
}) {
  const { field, errors, invalid, disabled, onBlur, onChange } =
    useFieldBinding<string>(false);
  const returned = useContext(ReturnedFieldsContext);
  const nameId = useId();
  const hintId = useId();
  const tested = returned !== null;

  return (
    <div
      className={cn(
        "flex flex-col gap-1.5 py-3 first:pt-0 last:pb-0",
        tested ? layouts.tested.row : layouts.plain.row,
      )}
    >
      {/* A control's height on the wide table, so the name sits on the
          input's centre line rather than at its top edge. */}
      <dt className="flex min-h-8.5 flex-col justify-center text-sm sm:min-h-7.5">
        <span id={nameId} className="font-medium text-foreground">
          {name}
        </span>
        {hint !== undefined && (
          <span id={hintId} className="text-muted-foreground text-xs">
            {hint}
          </span>
        )}
      </dt>
      <dd className="min-w-0">
        <Field disabled={disabled} invalid={invalid} name={field.name}>
          <Input
            className="font-mono text-base sm:text-[13px]"
            value={field.state.value}
            onValueChange={onChange}
            onBlur={onBlur}
            aria-labelledby={nameId}
            aria-describedby={hint === undefined ? undefined : hintId}
            aria-invalid={invalid || undefined}
            autoComplete="off"
            autoCapitalize="off"
            spellCheck={false}
            placeholder={placeholder}
          />
          {invalid && (
            <FieldError match>
              <FormMessages errors={errors} />
            </FieldError>
          )}
        </Field>
      </dd>
      {tested && (
        <dd
          data-claim-result={testField}
          className="min-w-0 break-words text-sm @2xl/claims:flex @2xl/claims:min-h-7.5 @2xl/claims:items-center"
        >
          <ReturnedClaim result={returned[testField]} />
        </dd>
      )}
    </div>
  );
}

/**
 * Where the latest test read one field, or that it found nothing there. A field
 * the summary does not list reads as a dash: absence of a record is not a
 * claim that the provider failed to send it.
 */
function ReturnedClaim({
  result,
}: {
  result: LatestConnectionTestFieldView | undefined;
}) {
  const { i18n } = useLingui();
  const prefix = (
    <span className="@2xl/claims:sr-only">
      <Trans id="admin.federation.claims.column.returned.field">
        Last test returned:
      </Trans>{" "}
    </span>
  );
  if (result === undefined) {
    return (
      <span className="text-muted-foreground">
        {prefix}
        <span aria-hidden="true">–</span>
        <span className="sr-only">
          <Trans id="admin.federation.claims.returned.unknown">
            not reported
          </Trans>
        </span>
      </span>
    );
  }
  if (!result.present) {
    return (
      <span className="font-medium text-warning-foreground">
        {prefix}
        <Trans id="admin.federation.claims.returned.missing">Missing</Trans>
      </span>
    );
  }
  const label = documentSourceLabel(result.source);
  const source = label === undefined ? result.source : i18n._(label);
  return (
    <span className="text-muted-foreground">
      {prefix}
      {source}
      {result.claim !== undefined && result.claim !== "" && (
        <>
          {" · "}
          <span className="font-mono text-foreground text-xs">
            {result.claim}
          </span>
        </>
      )}
    </span>
  );
}
