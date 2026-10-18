import { MoreHorizontalIcon } from "lucide-react";
import type { ReactNode } from "react";
import { Button } from "@/components/custom/Button";
import { getTopLayerContainer } from "@/components/custom/top-layer";
import { Menu, MenuPopup, MenuTrigger } from "@/components/ui/menu";

/**
 * A row's "More" menu: a ghost icon button that opens the row's secondary
 * actions. `label` names the button for assistive technology and should say
 * which row it belongs to, since a list repeats the control.
 */
export function RowMenu({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}) {
  return (
    <Menu>
      <MenuTrigger
        render={<Button variant="ghost" size="icon-sm" aria-label={label} />}
      >
        <MoreHorizontalIcon />
      </MenuTrigger>
      <MenuPopup
        align="end"
        portalProps={{ container: getTopLayerContainer() }}
      >
        {children}
      </MenuPopup>
    </Menu>
  );
}
