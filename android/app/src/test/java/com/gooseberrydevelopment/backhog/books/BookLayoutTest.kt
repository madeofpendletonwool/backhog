package com.gooseberrydevelopment.backhog.books

import com.gooseberrydevelopment.backhog.api.BlockStyle
import com.gooseberrydevelopment.backhog.api.StyleRun
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class BookLayoutTest {

    private fun block(text: String, kind: String? = null, bold: Boolean = false, sceneBreak: Boolean = false) =
        ReaderBlock(
            offset = 0,
            text = text,
            images = emptyList(),
            style = if (kind == null && !bold && !sceneBreak) {
                null
            } else {
                BlockStyle(kind = kind, sceneBreak = sceneBreak, runs = if (bold) listOf(StyleRun(text, "b")) else null)
            },
        )

    @Test
    fun `a chapter opener reads as label, title, subtitle, epigraph, attribution, opening`() {
        val laid = layoutBlocks(
            listOf(
                block("1.", "heading"),
                block("THE NAME GAME", "heading"),
                block("A Subtitle", "subtitle"),
                block("A line of verse,", "epigraph"),
                block("—A POET", "epigraph"),
                block("The prose begins."),
                block("And continues."),
                block("A Section", "heading"),
                block("After the section."),
                block("Before a break."),
                block("After the break.", sceneBreak = true),
            ),
        )
        assertEquals(
            listOf(
                BlockRole.Label, BlockRole.Title, BlockRole.Subtitle, BlockRole.Epigraph, BlockRole.Attribution,
                BlockRole.Lead, BlockRole.Body, BlockRole.Section, BlockRole.Lead, BlockRole.Body, BlockRole.Lead,
            ),
            laid.map { it.role },
        )
        assertTrue("the rule closes the opener", laid[4].endsOpener)
        assertTrue("only the first prose opens the chapter", laid[5].opening && laid.count { it.opening } == 1)
        assertTrue(laid[10].breakBefore)
        assertTrue(laid[3].groupStart && !laid[4].groupStart)
    }

    @Test
    fun `a part page's bold paragraph is its title, and a bold pull quote gets no flourish`() {
        val laid = layoutBlocks(
            listOf(
                block("Part I", "heading"),
                block("YOU AND ME", bold = true),
                block("A long bold question a reader wrote in, set as a pull quote on the part page itself.", bold = true),
            ),
        )
        assertEquals(listOf(BlockRole.Label, BlockRole.Title, BlockRole.Lead), laid.map { it.role })
        assertTrue(laid.none { it.opening })
    }

    @Test
    fun `plain books get plain paragraphs`() {
        val laid = layoutBlocks(listOf(block("One."), block("Two.")))
        assertEquals(listOf(BlockRole.Body, BlockRole.Body), laid.map { it.role })
        assertTrue(laid.none { it.opening })
    }

    @Test
    fun `pagination splits a tall paragraph across pages instead of clipping it`() {
        val pages = LinePages.paginate(
            listOf(
                LinePages.Item(List(3) { 10.0 }, marginBottom = 5.0),
                LinePages.Item(List(12) { 10.0 }, marginTop = 5.0),
            ),
            pageHeight = 50.0,
        )
        // Page 1: all of item 0 (30 + 5), then the 1 line of item 1 that fits after its margin.
        assertEquals(listOf(LinePages.Slice(0, 0, 3, 0.0, 5.0), LinePages.Slice(1, 0, 1, 5.0, 0.0)), pages[0])
        // The rest continues line by line, five lines a page, nothing lost.
        assertEquals(listOf(LinePages.Slice(1, 1, 6, 0.0, 0.0)), pages[1])
        assertEquals(listOf(LinePages.Slice(1, 6, 11, 0.0, 0.0)), pages[2])
        assertEquals(listOf(LinePages.Slice(1, 11, 12, 0.0, 0.0)), pages[3])
        assertEquals(15, pages.flatten().sumOf { it.lineEnd - it.lineStart })
        // Restoring to item 1 lands where its first line is.
        assertEquals(0, LinePages.pageForItem(pages, 1))
    }

    @Test
    fun `an item taller than a page gets a page of its own and the next item follows`() {
        val pages = LinePages.paginate(listOf(LinePages.Item(80.0), LinePages.Item(10.0)), pageHeight = 50.0)
        assertEquals(2, pages.size)
        assertEquals(0, LinePages.pageForItem(pages, 0))
        assertEquals(1, LinePages.pageForItem(pages, 1))
    }
}
