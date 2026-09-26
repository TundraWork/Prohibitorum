import { Tabs } from "@heroui/react";
import type { ReactNode } from "react";
import { AsyncSection } from "@/components/custom/Section";

export interface ConsoleTab<T extends string> {
  id: T;
  title: ReactNode;
  /** The panel, rendered only while its tab is selected. */
  panel: () => ReactNode;
}

/**
 * A console page's tabs, with the selected tab in the URL (see `tabs.ts`).
 *
 * Only the selected panel is mounted, so a panel's reads start when someone
 * opens it. Each panel is also its own loading and error boundary: a panel
 * that suspends on its data shows a spinner in its own place, and one that
 * fails says so there with a retry, while the tab strip and the other tabs stay
 * usable. That is why a tab's data belongs in the panel, not in the route: a
 * loader keyed on the tab turns every switch into a route transition, and the
 * route's pending component then replaces the whole page.
 *
 * The page supplies `onSelectionChange`, which navigates with `replace: true`
 * so browsing tabs never buries the page the user came from.
 */
export function ConsoleTabs<T extends string>({
  label,
  selected,
  onSelectionChange,
  tabs,
}: {
  label: string;
  selected: T;
  onSelectionChange: (tab: T) => void;
  tabs: readonly ConsoleTab<T>[];
}) {
  return (
    <Tabs
      selectedKey={selected}
      onSelectionChange={(key) => onSelectionChange(key as T)}
    >
      <Tabs.ListContainer className="ml-2 w-fit max-w-full">
        <Tabs.List aria-label={label}>
          {tabs.map((tab) => (
            <Tabs.Tab key={tab.id} className="whitespace-nowrap" id={tab.id}>
              {tab.title}
              <Tabs.Indicator />
            </Tabs.Tab>
          ))}
        </Tabs.List>
      </Tabs.ListContainer>
      {tabs.map((tab) => (
        <Tabs.Panel key={tab.id} id={tab.id} className="pt-4">
          {tab.id === selected && (
            <AsyncSection resetKey={tab.id}>{tab.panel()}</AsyncSection>
          )}
        </Tabs.Panel>
      ))}
    </Tabs>
  );
}
