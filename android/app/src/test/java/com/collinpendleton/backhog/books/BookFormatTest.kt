package com.collinpendleton.backhog.books

import com.collinpendleton.backhog.api.BookEdition
import com.collinpendleton.backhog.api.BookPosition
import com.collinpendleton.backhog.api.PositionChapter
import com.collinpendleton.backhog.api.PositionPage
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class BookFormatTest {

    private fun edition(id: String, isbn13: String = "", isbn10: String = "", publisher: String = "", year: Int? = null, pages: Int? = null) =
        BookEdition(id = id, isbn13 = isbn13, isbn10 = isbn10, publisher = publisher, publishedYear = year, pageCount = pages)

    @Test
    fun `isbn forms convert both ways`() {
        assertEquals("9780141187761", isbn10To13("014118776X"))
        assertEquals("014118776X", isbn13To10("978-0-14-118776-1"))
        assertEquals("080442957X", isbn13To10("9780804429573"))
        assertEquals("", isbn13To10("9791234567896"))
        assertEquals("", isbn10To13("12345"))
    }

    @Test
    fun `matchEditions finds a printing by any run of either isbn`() {
        val editions = listOf(
            edition("a", isbn13 = "9780141187761", publisher = "Penguin", year = 2000, pages = 480),
            edition("b", isbn10 = "0199535566", publisher = "Oxford", year = 2008, pages = 480),
            edition("c", publisher = "Penguin", year = 1985),
        )
        // The tail of the ISBN-13; the ISBN-10 of a printing filed only by its 13.
        assertEquals(listOf("a"), matchEditions(editions, "87761").map { it.id })
        assertEquals(listOf("a"), matchEditions(editions, "0-14-118776-x").map { it.id })
        // The ISBN-13 of a printing filed only by its 10.
        assertEquals(listOf("b"), matchEditions(editions, "9780199535569").map { it.id })
        // Words and numbers narrow together; a page count matches too.
        assertEquals(listOf("a", "c"), matchEditions(editions, "penguin").map { it.id })
        assertEquals(listOf("a"), matchEditions(editions, "penguin 480").map { it.id })
        assertEquals(listOf("a", "b", "c"), matchEditions(editions, "  ").map { it.id })
        assertEquals(emptyList<String>(), matchEditions(editions, "faber").map { it.id })
        // A short number ranks a page count above an incidental ISBN fragment.
        val pages = listOf(
            edition("isbn", isbn13 = "9783432090856", pages = 310),
            edition("pages", isbn13 = "9780000000002", pages = 320),
        )
        assertEquals(listOf("pages", "isbn"), matchEditions(pages, "320").map { it.id })
    }

    @Test
    fun `byline joins authors the way the web does`() {
        assertEquals("", byline(null))
        assertEquals("", byline(emptyList()))
        assertEquals("Le Guin", byline(listOf("Le Guin")))
        assertEquals("Gaiman & Pratchett", byline(listOf("Gaiman", "Pratchett")))
        assertEquals("A & 2 others", byline(listOf("A", "B", "C")))
    }

    @Test
    fun `isbn normalization strips separators and uppercases`() {
        assertEquals("9780141187761", normalizeIsbn("978-0-14-118776-1"))
        assertEquals("9780141187761", normalizeIsbn(" 978 0 14 118776 1 "))
        assertEquals("0199535566", normalizeIsbn("0-19-953556-6"))
    }

    @Test
    fun `looksLikeIsbn accepts isbn10 with X and isbn13`() {
        assertEquals(true, looksLikeIsbn("9780141187761"))
        assertEquals(true, looksLikeIsbn("019953556X"))
        assertEquals(true, looksLikeIsbn("0-19-953556-6"))
        assertEquals(false, looksLikeIsbn("019953556")) // too short
        assertEquals(false, looksLikeIsbn("01995355666")) // 11 digits
        assertEquals(false, looksLikeIsbn("not a book"))
    }

    @Test
    fun `formatPage carries the error bar exactly like the web`() {
        // measured: margin rounds to zero
        assertEquals("page 214", formatPage(PositionPage(page = 214, margin = 0.0)))
        // any fractional margin rounds up — a bar rounded down to zero reads as a promise
        assertEquals("page 214 ± 1", formatPage(PositionPage(page = 214, margin = 0.4)))
        assertEquals("page 214 ± 3", formatPage(PositionPage(page = 214, margin = 2.1)))
        assertEquals("page 214 ± 1", formatPage(PositionPage(page = 214, margin = 0.6)))
        // one anchor, no bound
        assertEquals("page ~214", formatPage(PositionPage(page = 214, margin = null)))
        assertNull(formatPage(null))
    }

    @Test
    fun `positionHeadline prefers page then chapter then percent`() {
        val chapter = PositionChapter(title = "Anathem", number = 3)

        assertEquals("Not started", positionHeadline(position(updatedAt = null), null))
        assertEquals("Finished", positionHeadline(position(percent = 100.0), null))
        assertEquals(
            "Page ~214 of 480",
            positionHeadline(position(percent = 44.0, page = PositionPage(page = 214, margin = null)), 480),
        )
        assertEquals("Anathem", positionHeadline(position(percent = 44.0, chapter = chapter), null))
        assertEquals("44% through", positionHeadline(position(percent = 44.0), null))
        assertEquals(
            "Page 5 of 120",
            positionHeadline(
                position(positionMode = "page", percent = 4.0, pageIndex = 4, pageCount = 120),
                null,
            ),
        )
    }

    @Test
    fun `timecode formats hours with padding`() {
        assertEquals("0:05", formatTimecode(5.0))
        assertEquals("12:05", formatTimecode(12 * 60 + 5.0))
        assertEquals("3:12:05", formatTimecode(3 * 3600 + 12 * 60 + 5.0))
        assertEquals("0:00", formatTimecode(-3.0))
    }

    @Test
    fun `edition picker label drops empty parts`() {
        val edition = BookEdition(
            id = "e",
            publisher = "Penguin",
            publishedYear = 1955,
            binding = "Paperback",
            pageCount = 328,
        )
        assertEquals("Penguin · 1955 · Paperback · 328 pages", editionLabel(edition))
        assertEquals("1 page", formatPages(1))
        assertEquals("", formatPages(null))
    }

    @Test
    fun `pageCountFor prefers the copy driving pages then the anchored edition`() {
        val withCount = edition("a", 300)
        val withoutCount = edition("b", null)
        val alsoCount = edition("c", 400)
        val editions = listOf(withoutCount, withCount, alsoCount)

        assertEquals(400, pageCountFor(editions, "c"))
        assertEquals(300, pageCountFor(editions, null, "a"))
        // falls through to the first printing with any count
        assertEquals(300, pageCountFor(listOf(withoutCount, withCount)))
        assertNull(pageCountFor(listOf(withoutCount)))
    }

    @Test
    fun `sortEditions puts counted printings first newest within`() {
        val old = edition("old", 200, 1970)
        val new = edition("new", 250, 2010)
        val uncounted = edition("uncounted", null, 2020)
        val sorted = sortEditions(listOf(old, uncounted, new))
        assertEquals(listOf("new", "old", "uncounted"), sorted.map { it.id })
    }

    private fun edition(id: String, pages: Int?, year: Int? = null) = BookEdition(
        id = id,
        pageCount = pages,
        publishedYear = year,
    )

    private fun position(
        percent: Double = 0.0,
        updatedAt: String? = "2026-01-01T00:00:00Z",
        page: PositionPage? = null,
        chapter: PositionChapter? = null,
        positionMode: String = "text",
        pageIndex: Int? = null,
        pageCount: Int = 0,
    ) = BookPosition(
        positionMode = positionMode,
        percent = percent,
        updatedAt = updatedAt,
        page = page,
        chapter = chapter,
        pageIndex = pageIndex,
        pageCount = pageCount,
    )
}
