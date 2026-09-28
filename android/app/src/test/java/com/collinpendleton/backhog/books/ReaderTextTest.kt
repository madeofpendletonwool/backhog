package com.collinpendleton.backhog.books

import com.collinpendleton.backhog.api.ChapterImage
import com.collinpendleton.backhog.api.TOCReport
import com.collinpendleton.backhog.api.TextChapter
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ReaderTextTest {

    private fun chapter(
        spine: Int,
        start: Long,
        end: Long,
        blocks: List<Long>? = emptyList(),
        images: List<ChapterImage> = emptyList(),
        title: String = "",
        titleSource: String = "toc",
        number: Int = 1,
        depth: Int = 0,
    ) = TextChapter(
        spineIndex = spine,
        charStart = start,
        charEnd = end,
        blocks = blocks,
        images = images,
        title = title,
        titleSource = titleSource,
        number = number,
        depth = depth,
    )

    @Test
    fun `readerBlocks pairs offsets with display text block for block`() {
        val c = chapter(0, 0, 100, blocks = listOf(0, 10, 40))
        val blocks = readerBlocks(c, listOf("alpha", "beta", "gamma"))
        assertEquals(3, blocks.size)
        assertEquals(0, blocks[0].offset)
        assertEquals("beta", blocks[1].text)
        assertEquals(40, blocks[2].offset)
    }

    @Test
    fun `readerBlocks pairs only as far as both agree`() {
        val c = chapter(0, 0, 100, blocks = listOf(0, 10, 40, 90))
        val blocks = readerBlocks(c, listOf("alpha", "beta"))
        assertEquals(2, blocks.size)
    }

    @Test
    fun `an image-only document gets one empty block for its art`() {
        val c = chapter(3, 500, 500, blocks = emptyList(), images = listOf(ChapterImage("images/cover.jpg", beforeBlock = 0)))
        val blocks = readerBlocks(c, emptyList())
        assertEquals(1, blocks.size)
        assertEquals(500, blocks[0].offset)
        assertEquals(1, blocks[0].images.size)
    }

    @Test
    fun `images bucket above their block and pull back onto the last`() {
        val c = chapter(
            1, 0, 100,
            blocks = listOf(0, 20),
            images = listOf(
                ChapterImage("a.png", beforeBlock = 1),
                ChapterImage("trailing.png", beforeBlock = 7),
            ),
        )
        val blocks = readerBlocks(c, listOf("one", "two"))
        assertEquals(0, blocks[0].images.size)
        assertEquals(2, blocks[1].images.size)
    }

    @Test
    fun `off-origin and protocol-relative hrefs never render`() {
        val c = chapter(
            1, 0, 100, blocks = listOf(0),
            images = listOf(
                ChapterImage("https://evil.example/x.png"),
                ChapterImage("//evil.example/x.png"),
                ChapterImage("data:image/png;base64,AAAA"),
                ChapterImage("../escape.png"),
                ChapterImage("images/ok.png"),
            ),
        )
        val blocks = readerBlocks(c, listOf("text"))
        assertEquals(listOf("images/ok.png"), blocks[0].images.map { it.href })
    }

    @Test
    fun `isInternalHref refuses everything off the archive`() {
        assertTrue(isInternalHref("images/plate 1.jpg"))
        assertTrue(isInternalHref("OEBPS/../x".replace("/../", "/x/")))
        assertFalse(isInternalHref(""))
        assertFalse(isInternalHref("/absolute.png"))
        assertFalse(isInternalHref("//host.png"))
        assertFalse(isInternalHref("http://host.png"))
        assertFalse(isInternalHref("a:b/c.png"))
        assertFalse(isInternalHref("../up.png"))
        assertFalse(isInternalHref("a/../../up.png"))
    }

    @Test
    fun `chapterAt finds the holding chapter and gives the book's end to the last`() {
        val chapters = listOf(
            chapter(0, 0, 100, number = 1),
            chapter(1, 100, 300, number = 2),
            chapter(2, 300, 300, number = 3), // holds no text
        )
        assertEquals(0, chapterAt(chapters, 0)!!.spineIndex)
        assertEquals(1, chapterAt(chapters, 100)!!.spineIndex)
        assertEquals(1, chapterAt(chapters, 299)!!.spineIndex)
        // Past every range: the last chapter that holds text owns the end.
        assertEquals(1, chapterAt(chapters, 5000)!!.spineIndex)
    }

    @Test
    fun `blockIndexAt is the last block at or before the offset`() {
        val blocks = listOf(
            ReaderBlock(0, "a", emptyList()),
            ReaderBlock(50, "b", emptyList()),
            ReaderBlock(120, "c", emptyList()),
        )
        assertEquals(0, blockIndexAt(blocks, 0))
        assertEquals(0, blockIndexAt(blocks, 49))
        assertEquals(1, blockIndexAt(blocks, 50))
        assertEquals(2, blockIndexAt(blocks, 500))
    }

    @Test
    fun `readableChapters drops the sections that hold no text`() {
        val chapters = listOf(
            chapter(0, 0, 0),
            chapter(1, 0, 100),
        )
        assertEquals(listOf(1), readableChapters(chapters).map { it.spineIndex })
    }

    @Test
    fun `an unnamed chapter falls back to its number`() {
        assertEquals("Section 4", chapterTitle(chapter(2, 0, 1, title = "", titleSource = "none", number = 4)))
        assertEquals("TheDoor", chapterTitle(chapter(2, 0, 1, title = "TheDoor")))
    }

    @Test
    fun `a title is inferred unless the table of contents said it`() {
        assertTrue(isInferredTitle(chapter(0, 0, 1, title = "From heading", titleSource = "heading")))
        assertFalse(isInferredTitle(chapter(0, 0, 1, title = "From toc", titleSource = "toc")))
        assertFalse(isInferredTitle(chapter(0, 0, 1, title = "", titleSource = "none")))
    }

    @Test
    fun `describeToc says how the chapter names were made`() {
        assertNull(describeToc(TOCReport(source = "nav", entries = 12), 0))
        assertNull(describeToc(null, 3))
        assertEquals(
            "This book's table of contents could not be read (bad ncx), so its chapter names were taken from the text itself.",
            describeToc(TOCReport(source = "", entries = 0, error = "bad ncx"), 0),
        )
        assertEquals(
            "This file carries no usable table of contents, so it was divided at its page breaks and the chapter names were taken from the text itself.",
            describeToc(TOCReport(source = "mobi-pagebreak", entries = 0), 0),
        )
        assertEquals(
            "This book has no table of contents, so its chapter names were taken from the text itself.",
            describeToc(TOCReport(source = "", entries = 0), 0),
        )
        assertEquals(
            "This book's table of contents has a single entry, so its chapter names were taken from the text itself.",
            describeToc(TOCReport(source = "nav", entries = 1), 0),
        )
        assertEquals(
            "This book's table of contents does not cover 3 of its chapters, so those names were taken from the text itself.",
            describeToc(TOCReport(source = "nav", entries = 12), 3),
        )
    }

    @Test
    fun `percentAt clamps to the book`() {
        val text = com.collinpendleton.backhog.api.BookTextChapters(charCount = 200)
        assertEquals(0.0, percentAt(text, 0), 0.001)
        assertEquals(50.0, percentAt(text, 100), 0.001)
        assertEquals(100.0, percentAt(text, 400), 0.001)
        assertEquals(0.0, percentAt(null, 50), 0.001)
    }
}
