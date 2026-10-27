package com.collinpendleton.backhog.ui.books.reader

import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.BaselineShift
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextIndent
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.TextUnit
import androidx.compose.ui.unit.em
import androidx.compose.ui.unit.sp
import com.collinpendleton.backhog.api.StyleRun
import com.collinpendleton.backhog.books.BlockRole
import com.collinpendleton.backhog.books.DISPLAY_ROLES
import com.collinpendleton.backhog.books.LaidBlock
import com.collinpendleton.backhog.books.ReaderBlock
import com.collinpendleton.backhog.data.ReaderPrefs

/**
 * How one block is set: the string with its marks, the paragraph style, and
 * the air and insets around it — everything both the measuring and the
 * drawing need. It is computed once and used by both, which is the whole
 * point: the old reader measured paragraphs one way and drew them another
 * (a different page height, a different paragraph gap), and every page that
 * came out taller than measured lost its last lines off the bottom.
 *
 * The roles and the proportions are the web reader's (index.css .bt-*), in
 * multiples of the reader's own text size so the page scales together.
 */
data class BlockLook(
    val text: AnnotatedString,
    val style: TextStyle,
    /** Space above and below, in multiples of the body text size. */
    val marginTopEm: Float,
    val marginBottomEm: Float,
    /** Horizontal inset each side (epigraphs, quotes), in em. */
    val insetEm: Float,
)

fun blockLook(block: ReaderBlock, laid: LaidBlock, base: TextStyle, prefs: ReaderPrefs, muted: Color): BlockLook {
    val role = laid.role
    val prose = role == BlockRole.Body || role == BlockRole.Lead
    val runs = block.style?.runs?.let { runs ->
        if (role in DISPLAY_ROLES) runs.map { it.copy(m = it.m.replace("b", "")) } else runs
    }
    val text = annotate(
        block.text,
        runs,
        uppercase = role == BlockRole.Label,
        openingCaps = laid.opening && prefs.opening,
        bullet = role == BlockRole.Item,
    )
    val f = base.fontSize
    fun scaled(by: Float): TextUnit = (f.value * by).sp
    val lineHeight = base.lineHeight

    val style = when (role) {
        BlockRole.Label -> base.copy(
            fontSize = scaled(0.78f), lineHeight = scaled(1.4f), letterSpacing = 0.24.em,
            textAlign = TextAlign.Center, color = muted,
        )
        BlockRole.Title -> base.copy(
            fontSize = scaled(1.9f), lineHeight = scaled(1.9f * 1.18f), fontWeight = FontWeight.SemiBold,
            textAlign = TextAlign.Center,
        )
        BlockRole.Subtitle -> base.copy(
            fontSize = scaled(1.1f), lineHeight = scaled(1.1f * 1.4f), fontStyle = FontStyle.Italic,
            textAlign = TextAlign.Center, color = muted,
        )
        BlockRole.Section -> base.copy(
            fontSize = scaled(1.1f), lineHeight = scaled(1.1f * 1.3f), fontWeight = FontWeight.SemiBold,
        )
        BlockRole.Epigraph -> base.copy(fontSize = scaled(0.92f), lineHeight = scaled(0.92f * 1.5f), fontStyle = FontStyle.Italic)
        BlockRole.Attribution -> base.copy(
            fontSize = scaled(0.78f), lineHeight = scaled(1.3f), letterSpacing = 0.08.em,
            textAlign = TextAlign.End, color = muted,
        )
        BlockRole.Quote -> base.copy(fontSize = scaled(0.95f), textAlign = if (prefs.justify) TextAlign.Justify else TextAlign.Start)
        BlockRole.Pre -> base.copy(fontFamily = FontFamily.Monospace, fontSize = scaled(0.85f))
        BlockRole.Caption -> base.copy(fontSize = scaled(0.85f), textAlign = TextAlign.Center, color = muted)
        BlockRole.Item -> base.copy(textIndent = TextIndent(restLine = 1.1.em))
        BlockRole.Lead, BlockRole.Body -> base.copy(
            textAlign = if (prefs.justify) TextAlign.Justify else TextAlign.Start,
            textIndent = if (role == BlockRole.Body && prefs.paragraphs == "indent") TextIndent(firstLine = 1.5.em) else null,
        )
    }.let { if (it.lineHeight == TextUnit.Unspecified) it.copy(lineHeight = lineHeight) else it }

    val spaced = prefs.paragraphs != "indent"
    val top = when (role) {
        BlockRole.Label -> 0.6f
        BlockRole.Title -> 0.5f
        BlockRole.Subtitle -> 0.4f
        BlockRole.Section -> 2f
        BlockRole.Epigraph -> if (laid.groupStart) 1.6f else 0f
        BlockRole.Attribution -> 0.5f
        BlockRole.Quote -> if (laid.groupStart) 0.9f else 0f
        BlockRole.Item -> 0.35f
        BlockRole.Pre -> 0.8f
        BlockRole.Caption -> 0.4f
        BlockRole.Lead, BlockRole.Body -> if (laid.afterInset) 0.9f else if (spaced) 0.85f else 0f
    }
    val bottom = when (role) {
        BlockRole.Section -> 0.6f
        BlockRole.Item -> 0.35f
        BlockRole.Pre -> 0.8f
        BlockRole.Caption -> 1.2f
        else -> 0f
    }
    val inset = when (role) {
        BlockRole.Epigraph, BlockRole.Attribution -> 2.2f
        BlockRole.Quote -> 1.6f
        BlockRole.Item -> 0.4f
        else -> 0f
    }
    return BlockLook(text, style, top, bottom, inset)
}

/**
 * The block's runs as an AnnotatedString. Small caps are drawn as reduced
 * capitals — the system serif has no small-caps feature to ask for — and a
 * <br> comes back as a real line break.
 */
private fun annotate(
    text: String,
    runs: List<StyleRun>?,
    uppercase: Boolean,
    openingCaps: Boolean,
    bullet: Boolean,
): AnnotatedString = buildAnnotatedString {
    if (bullet) append("• ")
    val source = runs ?: listOf(StyleRun(text))
    // The opening flourish: the first few words of a chapter in small caps.
    var capsLeft = if (openingCaps) openingLength(text) else 0
    for (run in source) {
        if ('n' in run.m) {
            append("\n")
            capsLeft = (capsLeft - run.t.length).coerceAtLeast(0)
            continue
        }
        val body = if (uppercase || 's' in run.m) run.t.uppercase() else run.t
        val caps = minOf(capsLeft, body.length)
        if (caps > 0) {
            withStyle(spanFor(run.m + "s")) { append(body.substring(0, caps).uppercase()) }
            capsLeft -= caps
        }
        if (caps < body.length) withStyle(spanFor(run.m)) { append(body.substring(caps)) }
    }
}

/** The opening flourish covers the first three words, or the first line's worth. */
private fun openingLength(text: String): Int {
    var words = 0
    for ((i, c) in text.withIndex()) {
        if (c == ' ') {
            words++
            if (words == 3 || i >= 24) return i
        }
    }
    return minOf(text.length, 24)
}

private fun spanFor(marks: String): SpanStyle {
    var span = SpanStyle()
    if ('b' in marks) span = span.merge(SpanStyle(fontWeight = FontWeight.SemiBold))
    if ('i' in marks) span = span.merge(SpanStyle(fontStyle = FontStyle.Italic))
    if ('s' in marks) span = span.merge(SpanStyle(fontSize = 0.8.em, letterSpacing = 0.05.em))
    if ('c' in marks) span = span.merge(SpanStyle(fontFamily = FontFamily.Monospace, fontSize = 0.85.em))
    if ('^' in marks) span = span.merge(SpanStyle(baselineShift = BaselineShift.Superscript, fontSize = 0.7.em))
    if ('_' in marks) span = span.merge(SpanStyle(baselineShift = BaselineShift.Subscript, fontSize = 0.7.em))
    return span
}
