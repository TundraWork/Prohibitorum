import { readFileSync } from "node:fs";
import type { Plugin } from "vite";

/** What a page needs beyond the shell's own script, by the group of routes it is in. */
export type RouteChunks = {
  /** The public routes' paths, `$name` standing for one segment. */
  publicPaths: string[];
  /** The files to preload for a page of each group, as the shell links them. */
  preload: Record<RouteGroup, string[]>;
};

type RouteGroup = "public" | "console";

/** Where the route files' code-split modules come from. */
const routeModule = /[\\/]src[\\/]routes[\\/]([^\\/?]+)\.tsx\?tsr-split=/;

/**
 * Bundles the pages into three pieces: what every page needs (the shell's own
 * script, with the router, the loaders and whatever both kinds of page share),
 * the public pages' code and the console's.
 *
 * The router splits each route's components into a dynamic import
 * (`autoCodeSplitting`). Left alone, that is one chunk per route, which a cold
 * load then fetches in waves; here each dynamically imported module is sorted
 * by what reaches it: a `_public` route's modules go to `public`, any other
 * route's (and any other dynamic import, such as the console's loading
 * skeleton) to `console`, and a module both reach joins the shell's script.
 *
 * It also writes `route-chunks.json`, which the server reads to add the right
 * group's `modulepreload` links to the shell for each address, so a page's
 * code is on its way with the shell's script instead of after it.
 */
export function routeChunks(routeTree: string): Plugin {
  const common = new Set<string>();
  const only: Record<RouteGroup, Set<string>> = {
    public: new Set(),
    console: new Set(),
  };
  return {
    name: "prohibitorum:route-chunks",
    apply: "build",
    buildEnd() {
      const reach = (roots: Iterable<string>) => {
        const seen = new Set<string>();
        const queue = [...roots];
        for (let id = queue.pop(); id !== undefined; id = queue.pop()) {
          if (seen.has(id)) continue;
          seen.add(id);
          queue.push(...(this.getModuleInfo(id)?.importedIds ?? []));
        }
        return seen;
      };
      const ids = [...this.getModuleIds()];
      const initial = reach(
        ids.filter((id) => this.getModuleInfo(id)?.isEntry),
      );
      const roots: Record<RouteGroup, string[]> = { public: [], console: [] };
      for (const id of ids) {
        for (const target of this.getModuleInfo(id)?.dynamicallyImportedIds ??
          []) {
          if (initial.has(target)) continue;
          const route = target.match(routeModule)?.[1];
          roots[route?.startsWith("_public") ? "public" : "console"].push(
            target,
          );
        }
      }
      const reached = {
        public: reach(roots.public),
        console: reach(roots.console),
      };
      for (const id of initial) common.add(id);
      for (const id of reached.public) {
        if (initial.has(id)) continue;
        if (reached.console.has(id)) common.add(id);
        else only.public.add(id);
      }
      for (const id of reached.console) {
        if (!initial.has(id) && !reached.public.has(id)) only.console.add(id);
      }
    },
    outputOptions(options) {
      return {
        ...options,
        codeSplitting: {
          groups: [
            { name: "index", test: (id) => common.has(id), priority: 3 },
            { name: "public", test: (id) => only.public.has(id), priority: 2 },
            {
              name: "console",
              test: (id) => only.console.has(id),
              priority: 1,
            },
          ],
        },
      };
    },
    generateBundle(_, bundle) {
      const chunks = Object.values(bundle).flatMap((file) =>
        file.type === "chunk" ? [file] : [],
      );
      const entry = chunks.find((chunk) => chunk.isEntry);
      const loaded = new Set([entry?.fileName, ...(entry?.imports ?? [])]);
      // A group's chunk and what it imports that the shell has not.
      const files = (group: RouteGroup) => {
        const out: string[] = [];
        const visit = (fileName: string) => {
          if (loaded.has(fileName) || out.includes(`/${fileName}`)) return;
          out.push(`/${fileName}`);
          for (const imported of chunks.find((c) => c.fileName === fileName)
            ?.imports ?? [])
            visit(imported);
        };
        for (const chunk of chunks)
          if (chunk.name === group) visit(chunk.fileName);
        return out;
      };
      const manifest: RouteChunks = {
        publicPaths: publicPaths(readFileSync(routeTree, "utf8")),
        preload: { public: files("public"), console: files("console") },
      };
      this.emitFile({
        type: "asset",
        fileName: "route-chunks.json",
        source: `${JSON.stringify(manifest, null, 2)}\n`,
      });
    },
  };
}

/**
 * The full paths of the routes under `/_public`, read from the router's
 * generated tree: each route's id and full path name the same route constant.
 */
export function publicPaths(tree: string): string[] {
  const entries = (name: string) => {
    const body = tree.match(
      new RegExp(`export interface ${name} \\{([^}]*)\\}`),
    )?.[1];
    if (body === undefined) throw new Error(`route tree has no ${name}`);
    return [...body.matchAll(/'([^']+)': typeof (\w+)/g)].map(
      (match) => [match[1] ?? "", match[2] ?? ""] as const,
    );
  };
  const publicRoutes = new Set(
    entries("FileRoutesById")
      .filter(([id]) => id.startsWith("/_public/"))
      .map(([, route]) => route),
  );
  return entries("FileRoutesByFullPath")
    .filter(([, route]) => publicRoutes.has(route))
    .map(([path]) => path);
}
