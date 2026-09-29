import { Avatar } from "@heroui/react";
import { useInstanceBranding } from "@/components/custom/instance-branding";

/**
 * The instance's icon and name, as the console sidebar and the public toolbar
 * both show them. The icon is decoration beside the name, so it has no text
 * of its own; without an icon the name's first letter stands in.
 */
export function InstanceIdentity() {
  const { name, iconUrl } = useInstanceBranding();
  return (
    <div className="flex min-w-0 items-center gap-3">
      <Avatar className="size-8 shrink-0 rounded-field">
        <Avatar.Image src={iconUrl} alt="" />
        <Avatar.Fallback className="rounded-field">
          {name.slice(0, 1)}
        </Avatar.Fallback>
      </Avatar>
      <span className="truncate text-sm font-semibold tracking-[-0.01em] text-foreground">
        {name}
      </span>
    </div>
  );
}
