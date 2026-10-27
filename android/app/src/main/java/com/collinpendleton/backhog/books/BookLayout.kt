package com.collinpendleton.backhog.books

/**
 * From the book's markup to the page's typography — a port of
 * web/src/lib/booklayout.ts, kept rule for rule so a chapter opens the same
 * way on the phone as in the browser.
 *
 * The server reports what each block *is* (a heading, a subtitle, a line of
 * an epigraph); this decides what each block is *for* on the page: an "1."
 * heading followed by a title heading is a chapter label, the paragraph
 * after the opener opens the chapter, and an epigraph's closing dash line is
 * its attribution.
 */
enum class BlockRole {
    Label, Title, Subtitle, Section, Epigraph, Attribution, Quote, Item, Pre, Caption, Lead, Body,
}

data class LaidBlock(
    val role: BlockRole = BlockRole.Body,
    /** Opens the chapter's prose: where the opening flourish goes. */
    val opening: Boolean = false,
    /** A scene-break ornament sits above it. */
    val breakBefore: Boolean = false,
    /** The chapter opener ends after this block: the rule goes below it. */
    val endsOpener: Boolean = false,
    /** The first of a run of same-role blocks — an epigraph's first line. */
    val groupStart: Boolean = false,
    /** Prose resuming after a quote or list. */
    val afterInset: Boolean = false,
)

private val LABEL_RE = Regex(
    "^(?:(?:chapter|part|book|section|act|volume|vol\\.?)\\s+)?(?:[\\divxlcdm]+|one|two|three|four|five|six|seven|eight|nine|ten|" +
        "eleven|twelve|thirteen|fourteen|fifteen|sixteen|seventeen|eighteen|nineteen|twenty)\\.?$" +
        "|^(?:chapter|part|book|prologue|epilogue|interlude|introduction)$",
    RegexOption.IGNORE_CASE,
)

private fun allBold(block: ReaderBlock): Boolean {
    val runs = block.style?.runs ?: return false
    return runs.all { it.t.isBlank() || 'b' in it.m }
}

private fun boldDisplayLine(block: ReaderBlock): Boolean = block.text.length <= 80 && allBold(block)

fun layoutBlocks(blocks: List<ReaderBlock>): List<LaidBlock> {
    val out = MutableList(blocks.size) { LaidBlock() }

    // The opener: the display matter at the top, before the first prose.
    var i = 0
    var sawTitle = false
    var sawHeading = false
    while (i < blocks.size) {
        val block = blocks[i]
        when (block.style?.kind) {
            "heading" -> {
                val next = blocks.getOrNull(i + 1)
                val label = !sawTitle && LABEL_RE.matches(block.text.trim()) &&
                    (next?.style?.kind == "heading" || (next != null && boldDisplayLine(next)))
                out[i] = out[i].copy(role = if (label) BlockRole.Label else if (sawTitle) BlockRole.Subtitle else BlockRole.Title)
                if (!label) sawTitle = true
                sawHeading = true
            }
            "subtitle" -> out[i] = out[i].copy(role = BlockRole.Subtitle)
            "epigraph" -> out[i] = out[i].copy(role = BlockRole.Epigraph)
            else -> {
                if (sawHeading && !sawTitle && boldDisplayLine(block)) {
                    out[i] = out[i].copy(role = BlockRole.Title)
                    sawTitle = true
                } else {
                    break
                }
            }
        }
        i++
    }
    val openerEnd = if (sawHeading) i else 0
    if (openerEnd > 0) out[openerEnd - 1] = out[openerEnd - 1].copy(endsOpener = true)

    var afterBreak = openerEnd > 0
    var openingPlaced = openerEnd == 0
    for (j in openerEnd until blocks.size) {
        val block = blocks[j]
        val style = block.style
        val brk = style?.sceneBreak == true
        if (brk) afterBreak = true
        val role = when (style?.kind) {
            "heading" -> BlockRole.Section.also { afterBreak = true }
            "subtitle" -> BlockRole.Subtitle
            "epigraph" -> BlockRole.Epigraph
            "quote" -> BlockRole.Quote
            "item" -> BlockRole.Item
            "pre" -> BlockRole.Pre
            "caption" -> BlockRole.Caption
            else -> null
        }
        if (role != null || block.text.isEmpty()) {
            out[j] = out[j].copy(role = role ?: BlockRole.Body, breakBefore = brk)
            continue
        }
        val opening = !openingPlaced && afterBreak && !allBold(block)
        if (opening) openingPlaced = true
        out[j] = out[j].copy(role = if (afterBreak) BlockRole.Lead else BlockRole.Body, breakBefore = brk, opening = opening)
        afterBreak = false
    }

    for (j in blocks.indices) {
        if (out[j].role != BlockRole.Epigraph) continue
        val last = out.getOrNull(j + 1)?.role != BlockRole.Epigraph
        if (last && blocks[j].text.trim().firstOrNull() in setOf('—', '―', '–', '-')) {
            out[j] = out[j].copy(role = BlockRole.Attribution)
        }
    }
    fun group(role: BlockRole?) = if (role == BlockRole.Attribution) BlockRole.Epigraph else role
    for (j in blocks.indices) {
        val role = group(out[j].role)
        val previous = group(out.getOrNull(j - 1)?.role)
        out[j] = out[j].copy(
            groupStart = role != previous || out[j].breakBefore,
            afterInset = role != previous && previous in setOf(BlockRole.Quote, BlockRole.Item, BlockRole.Pre),
        )
    }
    return out
}

/** Roles the reader sets the weight of, so the book's own bold is dropped. */
val DISPLAY_ROLES = setOf(BlockRole.Label, BlockRole.Title, BlockRole.Subtitle, BlockRole.Section, BlockRole.Attribution, BlockRole.Epigraph)

/**
 * Pagination that never clips: a block is a run of lines, and a block too
 * tall for what is left of a page continues on the next one, line by line.
 * Every page still reports a paragraph offset — the paragraph its first line
 * belongs to — which is the contract the reader's progress rides on.
 */
object LinePages {
    /** One block to place: its lines' heights, and the air above and below it. */
    data class Item(val lineHeights: List<Double>, val marginTop: Double = 0.0, val marginBottom: Double = 0.0) {
        /** An image or ornament: one unsplittable "line". */
        constructor(height: Double, marginTop: Double = 0.0, marginBottom: Double = 0.0) :
            this(listOf(height), marginTop, marginBottom)
    }

    /** Lines [lineStart, lineEnd) of item [item], placed on a page. */
    data class Slice(val item: Int, val lineStart: Int, val lineEnd: Int, val marginTop: Double, val marginBottom: Double) {
        val firstOfItem get() = lineStart == 0
    }

    fun paginate(items: List<Item>, pageHeight: Double): List<List<Slice>> {
        val capacity = pageHeight.coerceAtLeast(1.0)
        val pages = mutableListOf<List<Slice>>()
        var page = mutableListOf<Slice>()
        var used = 0.0
        fun newPage() {
            if (page.isNotEmpty()) pages.add(page)
            page = mutableListOf()
            used = 0.0
        }
        items.forEachIndexed { index, item ->
            var line = 0
            while (line < item.lineHeights.size) {
                // A margin at the top of a page is dropped, as in print.
                val top = if (page.isEmpty() || line > 0) 0.0 else item.marginTop
                var height = top
                var end = line
                while (end < item.lineHeights.size && used + height + item.lineHeights[end] <= capacity) {
                    height += item.lineHeights[end]
                    end++
                }
                if (end == line) {
                    if (page.isNotEmpty()) {
                        newPage()
                        continue
                    }
                    // A single line taller than a page (a huge image): it gets the page.
                    end = line + 1
                    height = item.lineHeights[line]
                }
                val bottom = if (end == item.lineHeights.size) item.marginBottom else 0.0
                page.add(Slice(index, line, end, top, bottom))
                used += height + bottom
                line = end
                if (line < item.lineHeights.size) newPage()
            }
        }
        newPage()
        return pages
    }

    /** The page holding the first line of [item]. */
    fun pageForItem(pages: List<List<Slice>>, item: Int): Int {
        pages.forEachIndexed { index, slices ->
            if (slices.any { it.item == item && it.firstOfItem }) return index
        }
        pages.forEachIndexed { index, slices ->
            if (slices.any { it.item >= item }) return index
        }
        return pages.lastIndex.coerceAtLeast(0)
    }
}
