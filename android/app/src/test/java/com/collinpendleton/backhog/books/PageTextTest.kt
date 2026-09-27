package com.collinpendleton.backhog.books

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/**
 * The extraction contract, ported from web/src/lib/ocr.ts: the longest
 * contiguous run of prose, hyphen joins repaired, the folio read off the
 * outermost lines with the foot winning ties.
 */
class PageTextTest {

    @Test
    fun `isProse excludes furniture and keeps real sentences`() {
        assertEquals(false, PageText.isProse("214")) // a bare folio
        assertEquals(false, PageText.isProse("CHAPTER SEVEN")) // a running head, short and shouty
        assertEquals(false, PageText.isProse("... :: ~~ ---")) // OCR gravel
        assertEquals(true, PageText.isProse("It was a bright cold day in April, and the clocks"))
        assertEquals(true, PageText.isProse("\"I don't know,\" she said quietly."))
    }

    @Test
    fun `longestProseRun throws the furniture away and repairs hyphens`() {
        val lines = listOf(
            "THE LONG PRICE",                                  // running head
            "214",                                             // folio
            "The imposition of an entirely new govern-",       // hyphenated break
            "ment was the subject of every conversation that spring,",
            "and everyone in the room could tell that much at least.",
            "15",                                              // the next thing the OCR read
        )
        assertEquals(
            "The imposition of an entirely new government was the subject of every conversation that spring, " +
                "and everyone in the room could tell that much at least.",
            PageText.longestProseRun(lines),
        )
    }

    @Test
    fun `longestProseRun keeps the longest contiguous run not the most lines`() {
        val lines = listOf(
            "One short line here ok.",   // prose (barely)
            "!!!",                        // breaks the run
            "A second run that is considerably longer than the first one and so wins the",
            "contest for the longest run of prose on the page by a comfortable margin.",
        )
        assertEquals(
            "A second run that is considerably longer than the first one and so wins the " +
                "contest for the longest run of prose on the page by a comfortable margin.",
            PageText.longestProseRun(lines),
        )
    }

    @Test
    fun `word cap truncates the passage at 220 words`() {
        val filler = List(300) { "lorem" }.joinToString(" ")
        val realLines = filler.split(" ").chunked(30).map { it.joinToString(" ") }
        val passage = PageText.longestProseRun(realLines)
        assertEquals(220, passage.split(" ").size)
    }

    @Test
    fun `folioIn prefers the foot and requires an end position`() {
        assertEquals(214, PageText.folioIn(listOf("The running head", "body text goes here and here", "", "214")))
        assertEquals(9, PageText.folioIn(listOf("9", "body text goes here and here")))
        // a number buried mid-line is not a folio
        assertNull(PageText.folioIn(listOf("he said 42 times over the course", "of the whole long evening")))
        assertNull(PageText.folioIn(emptyList()))
    }

    @Test
    fun `fromLines packages passage raw and folio together`() {
        val scan = PageText.fromLines(
            listOf(
                "A HEADER",
                "It was a bright cold day in April, and the clocks were striking thirteen.",
                "63",
            ),
        )
        assertEquals("It was a bright cold day in April, and the clocks were striking thirteen.", scan.passage)
        assertEquals(63, scan.pageNumber)
        assertEquals(3, scan.raw.lines().size)
    }
}
