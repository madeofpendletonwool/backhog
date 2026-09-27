package com.collinpendleton.backhog.books

/**
 * Reducing a photographed page to what the passage matcher wants.
 *
 * A port of the extraction half of web/src/lib/ocr.ts: the OCR engine
 * differs (ML Kit on-device here, Tesseract in the browser) but the output
 * contract is the same — the longest contiguous run of clean prose, with the
 * running head, the folio and the half-line of the next page thrown away,
 * because those are words that are not in a row in the book and the matcher's
 * shingles are built to reject exactly that.
 */
object PageText {

    /** The most words sent to the matcher; a match is certain long before this. */
    const val MAX_PASSAGE_WORDS = 220

    /** What one scan of one page produced. */
    data class PageScan(
        /** The longest run of clean prose found — what the matcher is given. */
        val passage: String,
        /** Everything the recogniser read, joined by lines. */
        val raw: String,
        /** A folio read off the head or foot of the page, when one looked like one. */
        val pageNumber: Int?,
    )

    /** Runs the extraction over the recogniser's lines, top to bottom. */
    fun fromLines(lines: List<String>): PageScan {
        val trimmed = lines.map { it.trim() }
        return PageScan(
            passage = longestProseRun(trimmed),
            raw = trimmed.filter { it.isNotEmpty() }.joinToString("\n"),
            pageNumber = folioIn(trimmed),
        )
    }

    /**
     * Picks the longest unbroken stretch of body text out of the recognised
     * lines. Hyphenated line breaks are the one join worth repairing:
     * "govern-\nment" is a word the book contains and "govern ment" is not.
     */
    fun longestProseRun(lines: List<String>): String {
        var best: List<String> = emptyList()
        var current: List<String> = emptyList()

        for (line in lines) {
            if (isProse(line)) {
                current = current + line
                if (wordCount(current) > wordCount(best)) best = current
            } else {
                current = emptyList()
            }
        }

        val joined = best
            .joinToString("\n")
            .replace(Regex("(\\w)-\\n(\\w)"), "$1$2")
            .replace(Regex("\\s*\\n\\s*"), " ")
            .replace(Regex("\\s+"), " ")
            .trim()

        val words = joined.split(" ")
        return if (words.size > MAX_PASSAGE_WORDS) words.take(MAX_PASSAGE_WORDS).joinToString(" ") else joined
    }

    /**
     * Whether one recognised line looks like a sentence rather than furniture
     * or noise: three words with a couple of letters each, mostly letters.
     * Enough to exclude a folio, a running head and a row of OCR gravel; lax
     * enough to keep dialogue and short final lines of a paragraph.
     */
    fun isProse(line: String): Boolean {
        if (line.length < 12) return false
        val words = line.split(Regex("\\s+")).filter { Regex("[A-Za-z]{2}").containsMatchIn(it) }
        if (words.size < 3) return false
        val glyphs = line.replace(Regex("\\s"), "")
        val letters = glyphs.replace(Regex("[^A-Za-z'’-]"), "")
        return letters.length.toDouble() / glyphs.length >= 0.7
    }

    private fun wordCount(lines: List<String>): Int =
        lines.sumOf { it.split(Regex("\\s+")).count { w -> w.isNotEmpty() } }

    /**
     * Reads the folio off the head or foot of the page. Page numbers live at
     * the very top or the very bottom, usually alone and occasionally beside a
     * running head, so only the outermost couple of lines are considered and
     * only a number sitting at one end of one of them counts. The foot wins
     * ties because most books number there.
     *
     * A suggestion, not an answer: the scan sheet fills the field in and lets
     * the reader correct it, because a folio misread as 213 instead of 218
     * would quietly poison the page map.
     */
    fun folioIn(lines: List<String>): Int? {
        val present = lines.filter { it.isNotEmpty() }
        if (present.isEmpty()) return null

        val candidates = present.takeLast(2).reversed() + present.take(2)
        for (line in candidates) {
            val match = Regex("^(\\d{1,4})\\b|\\b(\\d{1,4})$").find(line) ?: continue
            val page = (match.groupValues[1].ifEmpty { match.groupValues[2] }).toIntOrNull() ?: continue
            if (page in 1 until 10_000) return page
        }
        return null
    }
}
