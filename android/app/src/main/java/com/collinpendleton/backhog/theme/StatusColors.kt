package com.collinpendleton.backhog.theme

import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.lerp
import com.collinpendleton.backhog.api.Status

/**
 * Meaning-bearing colour, ported from StatusBadge.tsx and index.css.
 *
 * Status colour carries meaning — playing is cyan, played is green — and the
 * hue stays constant across every theme. Only the *lightness* may move: on a
 * light ground the 300-shade ink is pulled toward the theme's near-black by
 * the palette's toneDarken (0 on every dark ground, 62% on Paper).
 */
data class StatusTone(val tone: Color, val ink: Color)

val STATUS_TONES =
    mapOf(
        Status.BACKLOG to StatusTone(Color(0xFF64748B), Color(0xFFCBD5E1)), // slate-500 / 300
        Status.PLAYING to StatusTone(Color(0xFF06B6D4), Color(0xFF67E8F9)), // cyan-500 / 300
        Status.PLAYED to StatusTone(Color(0xFF10B981), Color(0xFF6EE7B7)), // emerald-500 / 300
        Status.DROPPED to StatusTone(Color(0xFFEF4444), Color(0xFFFCA5A5)), // red-500 / 300
        Status.IGNORED to StatusTone(Color(0xFF71717A), Color(0xFFD4D4D8)), // zinc-500 / 300
        Status.WISHLIST to StatusTone(Color(0xFFF59E0B), Color(0xFFFCD34D)), // amber-500 / 300
    )

/** The ink a status is written in, on this palette's ground. */
fun statusInk(status: Status, palette: BackhogPalette): Color {
    val ink = STATUS_TONES.getValue(status).ink
    return if (palette.toneDarken > 0f) lerp(ink, palette.c100, palette.toneDarken) else ink
}

/** Achievement tiers — muted medal tones, constant in every theme. */
enum class AchievementTier(val color: Color) {
    BRONZE(Color(0xFFD9A06B)),
    SILVER(Color(0xFFC4CCD8)),
    GOLD(Color(0xFFE6C35C)),
    LEGENDARY(Color(0xFF82E6FF)),
}
