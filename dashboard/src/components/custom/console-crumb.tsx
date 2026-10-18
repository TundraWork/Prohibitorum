import {
  createContext,
  type ReactNode,
  use,
  useEffect,
  useMemo,
  useState,
} from "react";

interface CrumbState {
  label: string | undefined;
  setLabel: (label: string | undefined) => void;
}

const CrumbContext = createContext<CrumbState | null>(null);

/**
 * Holds the name of the record a console page is about, so the header's
 * breadcrumb can end on it. The shell provides it around the page outlet.
 */
export function ConsoleCrumbProvider({
  children,
}: {
  children: ReactNode;
}): React.ReactElement {
  const [label, setLabel] = useState<string | undefined>();
  const value = useMemo(() => ({ label, setLabel }), [label]);
  return <CrumbContext value={value}>{children}</CrumbContext>;
}

/** The record name a page registered, or nothing on a page about no record. */
export function useConsoleCrumbLabel(): string | undefined {
  return use(CrumbContext)?.label;
}

/**
 * Names the record this page is about in the header's breadcrumb. The name is
 * dropped again when the page unmounts, so the next page starts clean.
 */
export function useConsoleCrumb(label: string | undefined): void {
  const setLabel = use(CrumbContext)?.setLabel;
  useEffect(() => {
    setLabel?.(label);
    return () => setLabel?.(undefined);
  }, [label, setLabel]);
}
