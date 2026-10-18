import {
  createContext,
  type ReactNode,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react";

export interface RegisteredSection {
  id: string;
  title: string;
  dirty: boolean;
  tone?: "danger";
}

interface Registry {
  sections: Record<string, RegisteredSection>;
  set: (section: RegisteredSection) => void;
  remove: (id: string) => void;
}

const RegistryContext = createContext<Registry | null>(null);

const same = (a: RegisteredSection, b: RegisteredSection) =>
  a.title === b.title && a.dirty === b.dirty && a.tone === b.tone;

/** Collects the page's `Section`s so the outline and the unsaved bar can list them. */
export function SectionRegistryProvider({
  children,
}: {
  children: ReactNode;
}): React.ReactElement {
  const [sections, setSections] = useState<Record<string, RegisteredSection>>(
    {},
  );
  const set = useCallback((section: RegisteredSection) => {
    setSections((prev) => {
      const existing = prev[section.id];
      return existing !== undefined && same(existing, section)
        ? prev
        : { ...prev, [section.id]: section };
    });
  }, []);
  const remove = useCallback((id: string) => {
    setSections((prev) => {
      if (prev[id] === undefined) return prev;
      const { [id]: _gone, ...rest } = prev;
      return rest;
    });
  }, []);
  const value = useMemo(
    () => ({ sections, set, remove }),
    [sections, set, remove],
  );
  return (
    <RegistryContext.Provider value={value}>
      {children}
    </RegistryContext.Provider>
  );
}

/** Registers one section; a no-op outside a provider or without a title yet. */
export function useRegisterSection(section: RegisteredSection): void {
  const registry = useContext(RegistryContext);
  const set = registry?.set;
  const remove = registry?.remove;
  const { id, title, dirty, tone } = section;
  useEffect(() => {
    if (title === "") return;
    set?.({ id, title, dirty, tone });
  }, [set, id, title, dirty, tone]);
  useEffect(() => () => remove?.(id), [remove, id]);
}

/** The registered sections in document order. */
export function useRegisteredSections(): RegisteredSection[] {
  const registry = useContext(RegistryContext);
  const sections = registry?.sections;
  return useMemo(() => {
    const position = (id: string) => document.getElementById(id);
    return Object.values(sections ?? {}).sort((a, b) => {
      const left = position(a.id);
      const right = position(b.id);
      if (left === null || right === null) return 0;
      return left.compareDocumentPosition(right) &
        Node.DOCUMENT_POSITION_FOLLOWING
        ? -1
        : 1;
    });
  }, [sections]);
}
