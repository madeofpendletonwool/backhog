import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { PAGE_SIZE } from "@/hooks/useLibrary";
import { api } from "@/lib/api";
import { isBookEntry, type BookEntry, type Status } from "@/lib/types";

/**
 * The books arena's read/write path. It is deliberately the same shape as
 * useLibrary — same TanStack Query, same page size, same invalidation set —
 * because the two arenas share one `library_entries` table and one cache: a
 * book going from "to read" to "read" moves the queue and the stats strip
 * exactly the way a game does.
 */

export interface BookLibraryParams {
  status?: string;
  q?: string;
  author?: string;
  subject?: string;
  language?: string;
  sort?: string;
}

/** Invalidates every view whose contents depend on book entry state. */
function invalidateBooks(queryClient: ReturnType<typeof useQueryClient>) {
  for (const key of ["library", "books", "queue", "stats", "bookStats", "bookFacets", "readingInsights", "readingDebt", "lists", "list", "entry", "projects", "project"]) {
    queryClient.invalidateQueries({ queryKey: [key] });
  }
}

/**
 * Paged book library query. `media=book` is pinned on every request: the
 * projection LEFT JOINs both subjects, so without it a shelf would come back
 * holding the games too.
 */
export function useBookLibrary(params: BookLibraryParams) {
  return useInfiniteQuery({
    queryKey: ["books", "library", params],
    initialPageParam: 0,
    queryFn: ({ pageParam }) =>
      api.library({
        ...(params as Record<string, string | number | undefined>),
        media: "book",
        limit: PAGE_SIZE,
        offset: pageParam,
      }),
    getNextPageParam: (lastPage, allPages) => {
      const loaded = allPages.reduce((count, page) => count + page.entries.length, 0);
      return loaded < lastPage.total ? loaded : undefined;
    },
    placeholderData: (previous) => previous, // keep the shelf stable while filtering
  });
}

/** `enabled` is for the sidebar, which only wants this in the books arena. */
export function useBookStats(enabled = true) {
  return useQuery({ queryKey: ["bookStats"], queryFn: api.bookStats, enabled });
}

export function useBookFacets() {
  return useQuery({ queryKey: ["bookFacets"], queryFn: api.bookFacets });
}

/**
 * "Your Reading Problem": the pages and hours you owe yourself, your measured
 * reading pace, and the superlatives. `enabled` mirrors useBookStats — the
 * shared debt page only wants this when the media filter is on books.
 */
export function useReadingInsights(enabled = true) {
  return useQuery({ queryKey: ["readingInsights"], queryFn: api.readingInsights, enabled });
}

export function useReadingDebt(enabled = true) {
  return useQuery({ queryKey: ["readingDebt"], queryFn: api.readingDebt, enabled });
}

/**
 * One book entry, narrowed. The entry route is shared across media, so a game
 * id typed into a /books/ URL resolves to an entry that simply is not a book;
 * returning undefined lets the page say so rather than render half a dossier.
 */
export function useBookEntry(id: string | undefined) {
  const query = useQuery({
    queryKey: ["entry", id],
    queryFn: () => api.getEntry(id!),
    enabled: Boolean(id),
  });
  const entry: BookEntry | undefined =
    query.data && isBookEntry(query.data) ? query.data : undefined;
  return { ...query, entry };
}

/** The work behind an entry, fetched for its editions list. */
export function useBook(bookId: string | undefined) {
  return useQuery({
    queryKey: ["book", bookId],
    queryFn: () => api.getBook(bookId!),
    enabled: Boolean(bookId),
    staleTime: 30 * 60 * 1000, // a work's printings do not change while you read
  });
}

export function useAddBookToLibrary() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      bookId,
      editionId,
      status,
    }: {
      bookId: string;
      editionId?: string | null;
      status?: Status;
    }) => api.addBookToLibrary(bookId, { editionId, status }),
    onSuccess: () => invalidateBooks(queryClient),
  });
}

/** Title/author search against the book provider, for the add dialog. */
export function useBookSearch(term: string) {
  return useQuery({
    queryKey: ["books", "search", term],
    queryFn: ({ signal }) => api.searchBooks(term, signal),
    enabled: term.trim().length >= 2,
    staleTime: 5 * 60 * 1000,
  });
}

/**
 * ISBN lookup. Enabled only once the barcode or the typed digits form a
 * plausible ISBN, so a half-typed number never spends a provider round trip.
 */
export function useBookByISBN(isbn: string, enabled: boolean) {
  return useQuery({
    queryKey: ["books", "isbn", isbn],
    queryFn: ({ signal }) => api.bookByISBN(isbn, signal),
    enabled: enabled && isbn.length > 0,
    retry: false, // a 404 here means "no such book", not a flaky network
    staleTime: 30 * 60 * 1000,
  });
}
