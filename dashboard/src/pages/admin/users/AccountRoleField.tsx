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
export function AccountRoleField({ savedRole }: { savedRole: string }) {
  const { t } = useLingui();
  const { field, errors, invalid, disabled, onChange } =
    useFieldBinding<string>(false);
  const labelId = useId();
  const known = savedRole === "user" || savedRole === "admin";
  const role = savedRole;

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
          },
          ...(known ? [] : [{ value: savedRole, label: savedRole }]),
        ]}
      />
      {!known && (
        <FieldDescription className="text-pretty">
          <Trans id="admin.user.role.unknown">
            This account has the role {role}, which the console does not offer
            and the server refuses. Change it to User or Admin before saving.
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
