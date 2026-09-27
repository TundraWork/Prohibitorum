import type { ReactNode } from "react";
import type { PrincipalSource } from "@/api/raw-admin-paths";
import { OptionSelectField } from "@/components/custom/OptionSelectField";
import { principalSources } from "@/components/custom/principal-sources";

/**
 * Which account fact a downstream application knows someone by, as a form
 * field: the OIDC subject source and the forward-auth `Remote-User`.
 *
 * Every option carries its cost under its name, because the choice is only as
 * good as the reader's picture of what each identifier does over time. The
 * field holds the choice like any other; a save that changes it is what the
 * section confirms, not the selection — a reader can look at each option and
 * change their mind without a dialog in the way.
 *
 * The select itself is `OptionSelectField`, which the identity provider's
 * two-line selects share, so the three cannot drift apart.
 */
export function PrincipalSourceField({
  label,
  isLabelHidden = false,
  description,
  className = "w-full",
}: {
  label: ReactNode;
  /**
   * For a field whose name is already on screen beside it, such as a header
   * table's own column: the label stays for assistive technology.
   */
  isLabelHidden?: boolean;
  description?: ReactNode;
  /**
   * Layout classes for the field. A select in a wide column takes its reading
   * width here rather than stretching across the column.
   */
  className?: string;
}) {
  return (
    <OptionSelectField<PrincipalSource>
      label={label}
      isLabelHidden={isLabelHidden}
      description={description}
      className={className}
      options={principalSources}
    />
  );
}
