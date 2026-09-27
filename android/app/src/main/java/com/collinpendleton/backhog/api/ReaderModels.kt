package com.collinpendleton.backhog.api

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

// The Stage 5 reader surface, transcribed from web/src/lib/types.ts (the
// source of truth). Offsets are UTF-8 byte offsets into the canonical text —
// opaque anchors, never string indexes.

/** One illustration of a spine document; `href` is a path inside the EPUB. */
@Serializable
data class ChapterImage(
    val href: String,
    val alt: String? = null,
    @SerialName("before_block") val beforeBlock: Int = 0,
)

/** Who named a chapter: "toc" | "heading" | "text" | "none". */

/** One chapter of the canonical text — a run of one or more spine documents. */
@Serializable
data class TextChapter(
    @SerialName("spine_index") val spineIndex: Int,
    val href: String = "",
    val title: String = "",
    @SerialName("title_source") val titleSource: String = "none",
    /** 1-based position among the chapters that hold text. */
    val number: Int = 0,
    @SerialName("char_start") val charStart: Long = 0,
    @SerialName("char_end") val charEnd: Long = 0,
    val depth: Int = 0,
    /** Null when the block-offset sidecar could not be read. */
    val blocks: List<Long>? = null,
    val images: List<ChapterImage> = emptyList(),
)

/** What became of the book's own table of contents. */
@Serializable
data class TOCReport(
    val source: String = "",
    val entries: Int = 0,
    /** Empty when the nav document could be read. */
    val error: String = "",
)

/** GET /api/books/{entryId}/text/chapters — the spine, with block offsets. */
@Serializable
data class BookTextChapters(
    @SerialName("char_count") val charCount: Long = 0,
    @SerialName("parser_version") val parserVersion: String = "",
    val chapters: List<TextChapter> = emptyList(),
    val toc: TOCReport = TOCReport(),
)

/** GET /api/books/{entryId}/text/display?spine=N — one spine document as prose. */
@Serializable
data class BookTextDisplay(
    @SerialName("spine_index") val spineIndex: Int = 0,
    val href: String = "",
    val blocks: List<String> = emptyList(),
)

/** One page of the paged reader's manifest: an image-native PDF's page axis. */
@Serializable
data class BookPageInfo(
    val index: Int = 0,
    val width: Int = 0,
    val height: Int = 0,
    @SerialName("has_image") val hasImage: Boolean = true,
)

/** GET /api/books/{entryId}/pages — the paged reader's manifest. */
@Serializable
data class BookPagesResponse(
    @SerialName("page_count") val pageCount: Int = 0,
    val pages: List<BookPageInfo> = emptyList(),
)

/** The PUT/POST position response: the new position as the server sees it. */
@Serializable
data class PositionWriteResult(val position: BookPosition? = null)
