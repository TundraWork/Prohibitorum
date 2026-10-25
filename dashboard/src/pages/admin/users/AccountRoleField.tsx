import { Trans, useLingui } from "@lingui/react/macro";
import { useId } from "react";
import { FormMessages } from "@/components/custom/FormMessages";
import { SegmentedControl } from "@/components/custom/SegmentedControl";
import { useFieldBinding } from "@/components/custom/use-field-binding";
import { Field, FieldDescription, FieldError } from "@/components/ui/field";

/**
 * The account's role as a form field: User or Admin.
 *
 * A stored role outside those two is shown as itself, as a third choice, with a
 * note, rather than as an empty control: the server only ever issues the two,
 * so any other value came from somewhere else and the server refuses it. The
 * note says it has to be changed before saving.
 */
export function AccountRoleField({
  savedRole,
  accountDisabled = false,
}: {
  savedRole: string;
  /** A disabled account cannot be an admin, so that choice is held. */
  accountDisabled?: boolean;
}) {
  const { t } = useLingui();
  const { field, errors, invalid, disabled, onChange } =
    useFieldBinding<string>(false);
  const labelId = useId();
  const holdId = useId();
  const known = savedRole === "user" || savedRole === "admin";
  const role = savedRole;
  const holdAdmin = accountDisabled && savedRole !== "admin";

  return (
    <Field name={field.name} invalid={invalid} disabled={disabled}>
      <span id={labelId} className="font-medium text-sm">
        <Trans id="admin.user.role">Role</Trans>
      </span>
      <SegmentedControl
        label={t({ id: "admin.user.role", message: "Role" })}
        labelledBy={labelId}
        value={field.state.value}
        disabled={disabled}
        size="default"
        onValueChange={onChange}
        options={[
          {
            value: "user",
            label: t({ id: "admin.user.role.user", message: "User" }),
          },
          {
            value: "admin",
            label: t({ id: "admin.user.role.admin", message: "Admin" }),
            disabled: holdAdmin,
            describedBy: holdAdmin ? holdId : undefined,
          },
          ...(known ? [] : [{ value: savedRole, label: savedRole }]),
        ]}
      />
      {holdAdmin && (
        <FieldDescription id={holdId} className="text-pretty">
          <Trans id="admin.user.role.admin-held">
            Enable the account before making it an admin.
          </Trans>
        </FieldDescription>
      )}
      {!known && (
        <FieldDescription className="text-pretty text-warning-foreground">
          <Trans id="admin.user.role.unknown">
            This account has the role {role}, which is not supported and is
            rejected by the server. Change it to User or Admin before saving.
          </Trans>
        </FieldDescription>
      )}
      {invalid && (
        <FieldError match>
          <FormMessages errors={errors} />
        </FieldError>
      )}
    </Field>
  );
}
