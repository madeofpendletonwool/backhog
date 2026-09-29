package com.collinpendleton.backhog.ui.books

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import coil3.compose.AsyncImage
import com.collinpendleton.backhog.api.BookStats
import com.collinpendleton.backhog.api.Entry
import com.collinpendleton.backhog.books.byline
import com.collinpendleton.backhog.data.ServerUrl
import com.collinpendleton.backhog.ui.theme.Backhog

/** The accent a card is tinted with; the server samples it from the cover. */
fun accentColor(hex: String?): Color {
    val clean = (hex ?: "").removePrefix("#")
    val value = clean.toLongOrNull(16) ?: 0x8B5CF6L
    return Color(value or 0xFF000000L)
}

/** Covers are served by the API from its own cache — public, cacheable images. */
fun bookCoverUrl(baseUrl: String, bookId: String): String =
    "${ServerUrl.apiRoot(baseUrl).trimEnd('/')}/covers/book/$bookId"

/**
 * A book's cover, or the tinted placeholder the web shows when Open Library
 * has none: the accent wash with the title set in it, so a shelf of unknown
 * covers still reads as a shelf. Takes the bare fields so it serves both the
 * full Book and the queue-facing BookBrief.
 */
@Composable
fun BookCover(title: String, coverUrl: String, accentHex: String, baseUrl: String, bookId: String, modifier: Modifier = Modifier) {
    val p = Backhog.palette
    val shape = RoundedCornerShape(6.dp)
    val accent = accentColor(accentHex)
    Box(
        modifier
            .clip(shape)
            .background(accent.copy(alpha = 0.18f).compositeOver(p.c850))
            .border(1.dp, p.edgeStrong, shape),
        contentAlignment = Alignment.Center,
    ) {
        if (coverUrl.isNotEmpty()) {
            AsyncImage(
                model = bookCoverUrl(baseUrl, bookId),
                contentDescription = title,
                contentScale = ContentScale.Crop,
                modifier = Modifier
                    .clip(shape)
                    .matchParentSize(),
            )
        } else {
            Text(
                title,
                color = p.toneInk(accent),
                style = MaterialTheme.typography.labelSmall,
                textAlign = TextAlign.Center,
                maxLines = 5,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.padding(8.dp),
            )
        }
    }
}

private fun Color.compositeOver(ground: Color): Color {
    val a = alpha
    return Color(
        red = red * a + ground.red * (1 - a),
        green = green * a + ground.green * (1 - a),
        blue = blue * a + ground.blue * (1 - a),
        alpha = 1f,
    )
}

/** The web's progress bar: a hairline track, the accent for a fill. */
@Composable
fun ProgressBar(percent: Float, modifier: Modifier = Modifier, fill: Color? = null) {
    val p = Backhog.palette
    Box(
        modifier
            .fillMaxWidth()
            .height(3.dp)
            .clip(RoundedCornerShape(2.dp))
            .background(p.c750),
    ) {
        Box(
            Modifier
                .fillMaxWidth(percent.coerceIn(0f, 1f))
                .height(3.dp)
                .background(fill ?: p.hlBright),
        )
    }
}

/** The shelf's strip: the six counts plus completion, one line of tiles. */
@Composable
fun StatsStrip(stats: BookStats?) {
    if (stats == null) return
    Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
        listOf(
            "Shelf" to stats.total,
            "To read" to stats.backlog,
            "Reading" to stats.reading,
            "Read" to stats.read,
            "Dropped" to stats.dropped,
        ).forEach { (label, count) -> StatTile(label, count, Modifier.weight(1f)) }
    }
}

@Composable
private fun StatTile(label: String, count: Int, modifier: Modifier = Modifier) {
    val p = Backhog.palette
    Column(
        modifier
            .clip(RoundedCornerShape(8.dp))
            .background(p.c850)
            .border(1.dp, p.edge, RoundedCornerShape(8.dp))
            .padding(vertical = 8.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Text(
            count.toString(),
            style = MaterialTheme.typography.titleMedium,
            fontWeight = FontWeight.SemiBold,
            color = p.c100,
        )
        Text(label, style = MaterialTheme.typography.labelSmall, color = p.c500)
    }
}

/** A labelled section heading, the web's `.section-label`. */
@Composable
fun SectionLabel(text: String) {
    val p = Backhog.palette
    Text(
        text.uppercase(),
        style = MaterialTheme.typography.labelMedium,
        letterSpacing = MaterialTheme.typography.labelMedium.letterSpacing * 1.4f,
        color = p.c500,
    )
}

/** "Colin's copy" — whose files this shared book is read through. */
@Composable
fun LenderBadge(sharedBy: String?) {
    if (sharedBy == null) return
    val p = Backhog.palette
    Text(
        "From $sharedBy",
        style = MaterialTheme.typography.labelSmall,
        color = p.hlBright,
        modifier = Modifier
            .clip(RoundedCornerShape(4.dp))
            .background(p.hlMid.copy(alpha = 0.15f))
            .padding(horizontal = 6.dp, vertical = 2.dp),
    )
}

/** A shelf card's caption lines: title, byline, and where the entry stands. */
@Composable
fun BookCaption(entry: Entry, compact: Boolean = false) {
    val p = Backhog.palette
    val book = entry.book ?: return
    val authors = byline(book.authors)
    Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Text(
            book.title,
            style = if (compact) MaterialTheme.typography.labelMedium else MaterialTheme.typography.titleSmall,
            color = p.c100,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
        )
        if (authors.isNotEmpty()) {
            Text(
                authors,
                style = MaterialTheme.typography.labelSmall,
                color = p.c400,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
        if (!compact) LenderBadge(entry.sharedBy)
    }
}

@Composable
fun EntryProgress(entry: Entry) {
    val percent = entry.progressPercent?.toFloat() ?: return
    if (percent <= 0f) return
    // The server sends 0–100; the bar takes a fraction.
    ProgressBar(percent / 100f)
}
