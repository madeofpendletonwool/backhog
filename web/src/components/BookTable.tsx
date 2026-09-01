import { Link } from "react-router-dom";

import { BookCover } from "./BookCover";
import { StatusBadge } from "./StatusBadge";
import { Gi } from "./ui/Gi";
import { accentStyle, byline, publishYear, relativeTime } from "@/lib/format";
import type { BookEntry } from "@/lib/types";

/** The dense alternative to the shelf, for scanning many books at once. */
export function BookTable({ entries }: { entries: BookEntry[] }) {
  return (
    <div className="panel overflow-hidden">
      <div className="overflow-x-auto">
        <table className="w-full min-w-[52rem] text-sm">
          <thead>
            <tr className="border-b border-white/[0.06] text-left text-xs font-medium text-ink-400">
              <th className="px-4 py-3 font-medium">Book</th>
              <th className="px-4 py-3 font-medium">Status</th>
              <th className="px-4 py-3 font-medium">Author</th>
              <th className="px-4 py-3 font-medium">Subjects</th>
              <th className="px-4 py-3 text-right font-medium">Rating</th>
              <th className="px-4 py-3 text-right font-medium">Added</th>
            </tr>
          </thead>
          <tbody>
            {entries.map((entry) => (
              <tr
                key={entry.id}
                className="border-b border-white/[0.04] transition-colors last:border-0 hover:bg-white/[0.03]"
              >
                <td className="px-4 py-2.5">
                  <Link
                    to={`/books/${entry.id}`}
                    style={accentStyle(entry.book)}
                    className="flex items-center gap-3 rounded-lg focus-visible:focus-ring"
                  >
                    <BookCover book={entry.book} className="w-8 shrink-0 rounded-md" />
                    <div className="min-w-0">
                      <p className="truncate font-medium text-ink-100">{entry.book.title}</p>
                      <p className="text-xs text-ink-500">{publishYear(entry.book) || "—"}</p>
                    </div>
                  </Link>
                </td>
                <td className="px-4 py-2.5">
                  <StatusBadge status={entry.status} media="book" />
                </td>
                <td className="max-w-[12rem] truncate px-4 py-2.5 text-xs text-ink-400">
                  {byline(entry.book) || "—"}
                </td>
                <td className="max-w-[14rem] truncate px-4 py-2.5 text-xs text-ink-400">
                  {entry.book.subjects.slice(0, 3).join(", ") || "—"}
                </td>
                <td className="px-4 py-2.5 text-right">
                  {entry.user_rating != null ? (
                    <span className="inline-flex items-center gap-1 tabular-nums text-amber-300">
                      <Gi name="star" className="size-3" />
                      {entry.user_rating}
                    </span>
                  ) : (
                    <span className="text-ink-600">—</span>
                  )}
                </td>
                <td className="px-4 py-2.5 text-right text-xs text-ink-500">
                  {relativeTime(entry.created_at)}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
