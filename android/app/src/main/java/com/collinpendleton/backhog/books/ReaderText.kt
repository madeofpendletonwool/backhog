package com.collinpendleton.backhog.books

import com.collinpendleton.backhog.api.BookTextChapters
import com.collinpendleton.backhog.api.ChapterImage
import com.collinpendleton.backhog.api.TOCReport
import com.collinpendleton.backhog.api.TextChapter
import kotlin.math.max
import kotlin.math.min

/**
 * The reader's coordinate maths, ported from web/src/lib/booktext.ts — the
 * part that has to be exactly right.
 *
 * A book comes back as two parallel things: `chapter.blocks` is the canonical
 * byte offset of every block in a spine document, and the display endpoint
 * returns the same blocks as prose. Block i of one *is* block i of the other.
 * That pairing is what lets a page show a paragraph and report an offset
 * that means it.
 *
 * Nothing here does byte arithmetic on a Kotlin string. Offsets are UTF-8
 * byte offsets and Kotlin strings are UTF-16, so they are opaque anchors.
 */

/** One rendered paragraph: what it says, and where it is. */
data class ReaderBlock(
    /** Absolute canonical byte offset of this block's first byte. */
    val offset: Long,
    val text: String,
    /** Illustrations that render above this block. */
    val images: List<ChapterImage>,
)

/**
 * Pairs a chapter's canonical offsets with its display text.
 *
 * A length mismatch means the sidecar and the display file disagree; rather
 * than render text under offsets that do not belong to it, it pairs only as
 * far as both agree. An image-only document owns no text but may still have
 * art, so it gets one empty block for the images to hang on.
 */
fun readerBlocks(chapter: TextChapter, display: List<String>): List<ReaderBlock> {
    val offsets = chapter.blocks ?: emptyList()
    val imagesAt = groupImages(chapter.images, offsets.size)

    if (offsets.isEmpty()) {
        val images = imagesAt[0] ?: emptyList()
        return if (images.isNotEmpty()) listOf(ReaderBlock(chapter.charStart, "", images)) else emptyList()
    }

    val paired = min(offsets.size, display.size)
    return buildList(paired) {
        for (i in 0 until paired) {
            add(ReaderBlock(offsets[i], display[i], imagesAt[i] ?: emptyList()))
        }
    }
}

/**
 * Buckets a chapter's images by the block they render above. Anything
 * anchored past the last block is pulled onto it, so a trailing plate still
 * appears rather than being dropped for pointing one past the end.
 */
private fun groupImages(images: List<ChapterImage>, blockCount: Int): Map<Int, List<ChapterImage>> {
    val grouped = mutableMapOf<Int, MutableList<ChapterImage>>()
    val last = max(0, blockCount - 1)
    for (image in images) {
        if (!isInternalHref(image.href)) continue
        val at = min(last, max(0, image.beforeBlock))
        grouped.getOrPut(at) { mutableListOf() }.add(image)
    }
    return grouped
}

/**
 * Whether an href is a plain relative path inside the EPUB. The parser
 * already refuses everything else and the asset endpoint refuses it again —
 * this is the third check, at the point where a string would become an image
 * request, so no single mistake upstream can turn a book into an off-origin
 * request.
 */
fun isInternalHref(href: String): Boolean {
    if (href.isEmpty() || href.startsWith("/") || href.startsWith("//")) return false
    val colon = href.indexOf(':')
    val slash = href.indexOf('/')
    if (colon >= 0 && (slash < 0 || colon < slash)) return false
    return !href.split('/').contains("..")
}

/**
 * The chapter holding an offset. Ranges are [char_start, char_end), so the
 * very end of the book belongs to the last chapter that holds any text.
 */
fun chapterAt(chapters: List<TextChapter>, offset: Long): TextChapter? {
    var last: TextChapter? = null
    for (chapter in chapters) {
        if (chapter.charEnd > chapter.charStart) last = chapter
        if (offset >= chapter.charStart && offset < chapter.charEnd) return chapter
    }
    return last
}

/** The index of the block containing an offset: the last one at or before it. */
fun blockIndexAt(blocks: List<ReaderBlock>, offset: Long): Int {
    var found = 0
    for (i in blocks.indices) {
        if (blocks[i].offset > offset) break
        found = i
    }
    return found
}

/** Chapters that hold text, which are the ones worth listing in a TOC. */
fun readableChapters(chapters: List<TextChapter>): List<TextChapter> =
    chapters.filter { it.charEnd > it.charStart }

/** A chapter's display name, falling back to its number when nothing named it. */
fun chapterTitle(chapter: TextChapter): String =
    chapter.title.trim().ifEmpty { "Section ${chapter.number}" }

/**
 * Whether a chapter's name was inferred by us rather than asserted by the
 * book. Titles read out of the markup are the book's own words but not what
 * its table of contents said, and one guessed from an opening line may be
 * wrong — the reader marks both.
 */
fun isInferredTitle(chapter: TextChapter): Boolean =
    chapter.title.isNotBlank() && chapter.titleSource != "toc"

/**
 * How a book's table of contents came out, in one sentence, or null when it
 * was fine — the difference between "94 numbered sections" and "the names
 * were read out of the text".
 */
fun describeToc(toc: TOCReport?, inferred: Int): String? {
    if (toc == null) return null
    if (toc.error.isNotBlank()) {
        return "This book's table of contents could not be read (${toc.error}), so its chapter names were taken from the text itself."
    }
    if (toc.source == "mobi-pagebreak") {
        return "This file carries no usable table of contents, so it was divided at its page breaks and the chapter names were taken from the text itself."
    }
    if (toc.entries == 0) {
        return "This book has no table of contents, so its chapter names were taken from the text itself."
    }
    if (toc.entries <= 1) {
        return "This book's table of contents has a single entry, so its chapter names were taken from the text itself."
    }
    if (inferred > 0) {
        return "This book's table of contents does not cover $inferred of its chapters, so those names were taken from the text itself."
    }
    return null
}

/** How far through the whole book an offset is, 0–100. */
fun percentAt(text: BookTextChapters?, offset: Long): Double {
    if (text == null || text.charCount <= 0) return 0.0
    return min(100.0, max(0.0, offset.toDouble() / text.charCount.toDouble() * 100.0))
}
