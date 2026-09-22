import { type InfiniteData, useInfiniteQuery } from "@tanstack/react-query";

/**
 * A server-paged list, accumulated as the reader keeps loading.
 *
 * Admin lists page by an opaque cursor the server issues, not by offset, so
 * there is no page number to jump to and no total to count against: the rows
 * already fetched are the rows there are. `nextCursor` is an empty string on
 * the last page, never a missing field, so that is the end-of-list test.
 *
 * The cursor is threaded through as the page parameter and never reaches the
 * URL: a shared link should land on the question that was asked, not in the
 * middle of its answer.
 */
export interface CursorPage<T> {
  items: T[] | null;
  nextCursor: string;
}

/**
 * The infinite-query options for one cursor-paged endpoint. The page parameter
 * is the cursor the server issued, or `undefined` for the first page — never an
 * empty string, which is what the last page reports back.
 */
export function cursorListOptions<T>(options: {
  queryKey: readonly unknown[];
  queryFn: (args: {
    cursor?: string;
    signal: AbortSignal;
  }) => Promise<CursorPage<T>>;
}) {
  return {
    queryKey: options.queryKey,
    initialPageParam: undefined as string | undefined,
    queryFn: ({
      pageParam,
      signal,
    }: {
      pageParam: string | undefined;
      signal: AbortSignal;
    }) =>
      options.queryFn({
        ...(pageParam === undefined ? {} : { cursor: pageParam }),
        signal,
      }),
    getNextPageParam: (last: CursorPage<T>) => last.nextCursor || undefined,
  };
}

export function useCursorList<T>(options: {
  queryKey: readonly unknown[];
  queryFn: (args: {
    cursor?: string;
    signal: AbortSignal;
  }) => Promise<CursorPage<T>>;
}) {
  const query = useInfiniteQuery<
    CursorPage<T>,
    Error,
    InfiniteData<CursorPage<T>>,
    readonly unknown[],
    string | undefined
  >(cursorListOptions(options));

  const pages = query.data?.pages ?? [];
  return {
    items: pages.flatMap((page) => page.items ?? []),
    /** The first page is still on its way; there is nothing to draw yet. */
    loading: query.isPending,
    /** A later page is on its way; the rows in hand stay on screen. */
    loadingMore: query.isFetchingNextPage,
    hasMore: query.hasNextPage,
    loadMore: () => {
      if (query.hasNextPage && !query.isFetchingNextPage) {
        void query.fetchNextPage();
      }
    },
    error: query.error,
  };
}
