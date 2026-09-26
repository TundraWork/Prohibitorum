import { msg } from "@lingui/core/macro";
import { useLingui } from "@lingui/react/macro";
import { Pencil } from "lucide-react";
import { Button } from "@/components/custom/Button";

const openProviderMessage = msg({
  id: "admin.federation.edit",
  message: "Open {name}",
});

/**
 * The actions a provider row offers.
 *
 * Opening a provider is the only thing a row does: enabling, rotating a secret
 * and deleting are all changes that want the provider's full state in front of
 * the reader, and the detail page is where that state lives. Keeping the row to
 * one control means a mis-aimed click cannot disable sign-in for everyone.
 */
export function ProviderActions({
  slug,
  displayName,
  onEdit,
}: {
  slug: string;
  displayName: string;
  onEdit: () => void;
}) {
  const { i18n } = useLingui();
  return (
    <Button
      isIconOnly
      size="sm"
      variant="ghost"
      aria-label={i18n._({
        ...openProviderMessage,
        values: { name: displayName || slug },
      })}
      onPress={onEdit}
    >
      <Pencil size={16} aria-hidden="true" />
    </Button>
  );
}
