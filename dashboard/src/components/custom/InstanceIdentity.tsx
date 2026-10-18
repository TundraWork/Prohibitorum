import { useInstanceBranding } from "@/components/custom/instance-branding";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";

/**
 * The instance's mark and name, as the public toolbar shows them. The mark is
 * decoration beside the name, so it has no text of its own; without an icon
 * the name's first letter stands in.
 */
export function InstanceIdentity() {
  const { name, iconUrl } = useInstanceBranding();
  return (
    <>
      <Avatar className="size-6 rounded-md">
        <AvatarImage src={iconUrl} alt="" />
        <AvatarFallback className="rounded-md bg-primary font-semibold text-primary-foreground">
          {name.slice(0, 1)}
        </AvatarFallback>
      </Avatar>
      <span className="truncate">{name}</span>
    </>
  );
}
