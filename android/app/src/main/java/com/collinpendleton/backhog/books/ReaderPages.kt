package com.collinpendleton.backhog.books

/**
 * The pagination spine: turning a column of measured blocks into pages, and
 * mapping between pages and the paragraph offsets that are the book's truth.
 *
 * Pure functions over heights, so the measuring (Compose, screen-sized) and
 * the maths (this file, unit-tested) stay separable. A page is a run of
 * items whose measured heights fit one viewport; an item taller than the
 * viewport gets a page of its own, because the alternative — splitting a
 * paragraph across a swipe — would break paragraph-level offset reporting,
 * which is the whole contract of this reader.
 */
object ReaderPages {

    /**
     * Groups item heights into pages. Returns one `IntRange` of item indexes
     * per page, in order; every item appears exactly once. An empty input
     * paginates to nothing.
     *
     * `firstLineAllowance` is extra room the first item on a page gives back
     * (the reading inset), measured in the same units as the heights.
     */
    fun paginate(heights: List<Double>, pageHeight: Double, firstLineAllowance: Double = 0.0): List<IntRange> {
        if (heights.isEmpty()) return emptyList()
        val capacity = pageHeight.coerceAtLeast(1.0)
        val pages = mutableListOf<IntRange>()
        var start = 0
        var used = 0.0
        for (i in heights.indices) {
            val h = heights[i].coerceAtLeast(0.0)
            val room = if (i == start) capacity - firstLineAllowance else capacity - used
            if (i > start && h > room) {
                pages.add(start until i)
                start = i
                used = 0.0
            }
            if (h > capacity) {
                // A single item taller than a whole page: it gets the page,
                // clipped by the renderer rather than split by the pager.
                pages.add(i..i)
                start = i + 1
                used = 0.0
            } else {
                used += h
            }
        }
        if (start < heights.size) pages.add(start until heights.size)
        return pages
    }

    /** The index of the page holding item [itemIndex]. */
    fun pageForItem(pages: List<IntRange>, itemIndex: Int): Int {
        for ((pageIndex, range) in pages.withIndex()) {
            if (itemIndex in range) return pageIndex
        }
        return pages.lastIndex.coerceAtLeast(0)
    }
}
