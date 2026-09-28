package com.collinpendleton.backhog.books

import org.junit.Assert.assertEquals
import org.junit.Test

class ReaderPagesTest {

    @Test
    fun `items fill pages up to the viewport`() {
        val pages = ReaderPages.paginate(listOf(10.0, 10.0, 10.0, 10.0), pageHeight = 25.0)
        // 10+10 fits, 10+10+10 does not: [0,1], [2,3]
        assertEquals(listOf(0..1, 2..3), pages)
    }

    @Test
    fun `every item appears exactly once in order`() {
        val heights = List(37) { (it % 5 + 1) * 7.0 }
        val pages = ReaderPages.paginate(heights, pageHeight = 60.0)
        val flattened = pages.flatten()
        assertEquals((0 until 37).toList(), flattened)
    }

    @Test
    fun `an oversized item gets a page of its own`() {
        val pages = ReaderPages.paginate(listOf(10.0, 100.0, 10.0), pageHeight = 50.0)
        // The 100-height item cannot fit any page: it gets one, clipped by the renderer.
        assertEquals(listOf(0..0, 1..1, 2..2), pages)
    }

    @Test
    fun `a single page holds everything that fits`() {
        val pages = ReaderPages.paginate(listOf(5.0, 5.0, 5.0), pageHeight = 100.0)
        assertEquals(listOf(0..2), pages)
    }

    @Test
    fun `empty input paginates to nothing`() {
        assertEquals(emptyList<IntRange>(), ReaderPages.paginate(emptyList(), 100.0))
    }

    @Test
    fun `pageForItem finds the page holding an item`() {
        val pages = ReaderPages.paginate(listOf(10.0, 10.0, 10.0, 10.0), pageHeight = 25.0)
        assertEquals(0, ReaderPages.pageForItem(pages, 0))
        assertEquals(0, ReaderPages.pageForItem(pages, 1))
        assertEquals(1, ReaderPages.pageForItem(pages, 2))
        assertEquals(1, ReaderPages.pageForItem(pages, 3))
    }

    @Test
    fun `pageForItem past the end lands on the last page`() {
        val pages = ReaderPages.paginate(listOf(10.0, 10.0), pageHeight = 25.0)
        assertEquals(0, ReaderPages.pageForItem(pages, 99))
        assertEquals(0, ReaderPages.pageForItem(emptyList(), 0))
    }
}
