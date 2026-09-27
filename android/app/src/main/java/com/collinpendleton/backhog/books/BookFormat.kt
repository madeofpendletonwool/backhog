package com.collinpendleton.backhog.books

import com.collinpendleton.backhog.api.Book
import com.collinpendleton.backhog.api.BookEdition
import com.collinpendleton.backhog.api.PositionChapter
import com.collinpendleton.backhog.api.PositionPage
import kotlin.math.ceil
import kotlin.math.roundToInt

// Ports of the web's display helpers (web/src/lib/format.ts, booktext.ts).
// Pure functions, unit-tested: the words the apps put on a page are part of
// their contract with each other.

/** "Ursula K. Le Guin", "Gaiman & Pratchett", "" — the byline, one line long. */
fun byline(book: Book): String {
    val authors = book.authors ?: return ""
    return when (authors.size) {
        0 -> ""
        1 -> authors[0]
        2 -> "${authors[0]} & ${authors[1]}"
        else -> "${authors[0]} & ${authors.size - 1} others"
    }
}

/** The year the work first appeared, as a string; "" when unknown. */
fun publishYear(book: Book): String = book.firstPublishYear?.toString() ?: ""

/**
 * Strips the separators people type and scanners emit, so "978-0-14-118776-1"
 * and "9780141187761" are the same lookup. Validation stays the API's job.
 */
fun normalizeIsbn(raw: String): String = raw.filter { !it.isWhitespace() && it != '-' }.uppercase()

/** ISBN-10 (last digit may be X) or ISBN-13, once separators are gone. */
fun looksLikeIsbn(raw: String): Boolean {
    val isbn = normalizeIsbn(raw)
    return Regex("^\\d{9}[\\dX]$").matches(isbn) || Regex("^\\d{13}$").matches(isbn)
}

/** The ISBN to show for a printing; ISBN-13 wins when both are known. */
fun editionIsbn(edition: BookEdition): String = edition.isbn13.ifEmpty { edition.isbn10 }

fun formatPages(pages: Int?): String {
    if (pages == null || pages <= 0) return ""
    return "%,d page%s".format(pages, if (pages == 1) "" else "s")
}

/** A one-line description of a printing for the edition picker. */
fun editionLabel(edition: BookEdition): String = listOf(
    edition.publisher,
    edition.publishedYear?.toString().orEmpty(),
    edition.binding,
    formatPages(edition.pageCount),
).filter { it.isNotBlank() }.joinToString(" · ")

/** Newest printings first, then the ones with a page count — the useful ones. */
fun sortEditions(editions: List<BookEdition>): List<BookEdition> = editions.sortedWith(
    compareByDescending<BookEdition> { it.pageCount != null }.thenByDescending { it.publishedYear ?: 0 },
)

/**
 * The page count "page 214 of N" is measured in: the copy driving the page
 * map, else the printing the entry is anchored to, else the first printing
 * on file that has a count at all.
 */
fun pageCountFor(editions: List<BookEdition>, vararg preferred: String?): Int? {
    for (id in preferred) {
        if (id.isNullOrEmpty()) continue
        editions.firstOrNull { it.id == id }?.pageCount?.let { return it }
    }
    return editions.firstOrNull { it.pageCount != null }?.pageCount
}

/** "2.5h" / "14h" — hours the way the dashboards say them. */
fun formatHours(hours: Double): String = when {
    hours <= 0.0 -> "0h"
    hours < 10.0 -> String.format(java.util.Locale.US, "%.1fh", hours)
    else -> "${hours.roundToInt()}h"
}

/** "3:12:05" / "12:05" — a place on the tape. */
fun formatTimecode(seconds: Double): String {
    val total = seconds.toInt().coerceAtLeast(0)
    val h = total / 3600
    val m = (total % 3600) / 60
    val s = total % 60
    return if (h > 0) "%d:%02d:%02d".format(h, m, s) else "%d:%02d".format(m, s)
}

/**
 * The printed page, said out loud with its error bar: "page 214 ± 3".
 *
 * Three shapes, in decreasing order of certainty:
 *   page 214      — the reader scanned this page; it was measured, not derived
 *   page 214 ± 3  — interpolated, and the map can bound how wrong it might be
 *   page ~214     — one anchor and no scale, so there is no bound to state
 */
fun formatPage(page: PositionPage?): String? {
    if (page == null) return null
    if (page.margin == null) return "page ~${page.page}"
    // Rounded up, because a bar rounded down to zero reads as a promise.
    val margin = ceil(page.margin).toInt()
    return if (margin > 0) "page ${page.page} ± $margin" else "page ${page.page}"
}

/** The same estimate explained, for a caption under the number. */
fun explainPage(page: PositionPage?): String? {
    if (page == null) return null
    if (page.margin == null) {
        return "Only one page of this printing has been scanned, so there is no way to tell how fast its pages go by. Scan another and this gets a real accuracy."
    }
    if (ceil(page.margin).toInt() == 0) return "You scanned this page, so this is where you are."
    val distance = page.anchorDistance.roundToInt()
    return "Estimated from the pages you have scanned; the nearest is about $distance page${if (distance == 1) "" else "s"} away. Scanning more tightens it."
}

/** A chapter's name, or "Section N" when the book did not give it one. */
fun chapterTitle(chapter: PositionChapter): String =
    chapter.title.trim().ifEmpty { "Section ${chapter.number}" }

/** Which end of the app last said where you are. */
fun describeSource(source: String): String = when (source) {
    "scan" -> "From a scanned page"
    "read" -> "From the reader"
    "listen" -> "From the audiobook"
    else -> "Set by hand"
}

/**
 * The one line that answers "where am I", in whichever space is most
 * concrete: the printed page beats the chapter beats the bare percentage.
 */
fun positionHeadline(position: com.collinpendleton.backhog.api.BookPosition, pageCount: Int?): String {
    if (position.positionMode == "page") {
        if (position.updatedAt == null) return "Not started"
        return "Page ${(position.pageIndex ?: 0) + 1} of ${position.pageCount}"
    }
    if (position.updatedAt == null || position.percent <= 0.0) return "Not started"
    if (position.percent >= 100.0) return "Finished"
    position.page?.let { page ->
        val text = formatPage(page) ?: ""
        return if (pageCount != null) "${text.replaceFirstChar { it.uppercase() }} of $pageCount"
        else text.replaceFirstChar { it.uppercase() }
    }
    position.chapter?.let { return chapterTitle(it) }
    return "${position.percent.roundToInt()}% through"
}
