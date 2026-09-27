package com.collinpendleton.backhog.ui.theme

import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.lerp

/** A theme family: how the chrome is built, not just its colours. Arcade is deferred past v1. */
enum class ThemeFamily(val label: String, val blurb: String) {
    Flat("Midnight", "Quiet dark chrome. Rounded surfaces, one violet accent, nothing between you and the covers."),
    Library("Library", "Ink on paper. Serif type, rules instead of boxes, and the page you are on marked in the margin."),
}

/** One palette inside a family. Keys match the web's theme ids so the two read the same. */
enum class ThemeId(val key: String, val label: String, val note: String, val family: ThemeFamily) {
    Midnight("midnight", "Midnight", "The original dark", ThemeFamily.Flat),
    Paper("paper", "Paper", "Warm daylight", ThemeFamily.Library),
    Hearth("hearth", "Hearth", "Read by the fire", ThemeFamily.Library);

    val palette: Palette
        get() = when (this) {
            Midnight -> Palettes.Midnight
            Paper -> Palettes.Paper
            Hearth -> Palettes.Hearth
        }

    companion object {
        val Default = Midnight

        /** Unknown ids — including the web's arcade themes — fall back to the default. */
        fun fromKey(key: String?): ThemeId? = entries.firstOrNull { it.key == key }
    }
}

/**
 * A port of the web's token ladder (web/src/themes/flat.css, library.css,
 * index.css). `c950` is the deepest ground and `c100` the strongest ink on
 * every theme, light or dark, so components name a step and each palette
 * answers — the text steps are the web's contrast-solved values, not
 * re-derived here.
 */
data class Palette(
    val isLight: Boolean,
    val c950: Color,
    val c900: Color,
    val c850: Color,
    val c800: Color,
    val c750: Color,
    val c700: Color,
    val c600: Color,
    val c500: Color,
    val c400: Color,
    val c300: Color,
    val c200: Color,
    val c100: Color,
    val cMax: Color,
    val line: Color,
    val hlPale: Color,
    val hlBright: Color,
    val hlMid: Color,
    val hlDim: Color,
    /** Ink on a highlight fill (primary buttons). */
    val hlInk: Color,
    val edgeSoft: Color,
    val edge: Color,
    val edgeStrong: Color,
    val fillHover: Color,
    val fillActive: Color,
    /** How far status/tier inks are pulled toward [c100]; 0 on dark grounds. */
    val toneDarken: Float,
    val serifDisplay: Boolean,
) {
    /** A status or tier hue as readable text on this ground — the web's `--tone-darken` mix. */
    fun toneInk(tone: Color): Color = lerp(tone, c100, toneDarken)
}

private fun white(alpha: Float) = Color.White.copy(alpha = alpha)
private fun warm(alpha: Float) = Color(0xFFFFE9CD).copy(alpha = alpha)
private fun umber(alpha: Float) = Color(0xFF3A2C1A).copy(alpha = alpha)

object Palettes {
    val Midnight = Palette(
        isLight = false,
        c950 = Color(0xFF08080A),
        c900 = Color(0xFF0C0C10),
        c850 = Color(0xFF121218),
        c800 = Color(0xFF17171F),
        c750 = Color(0xFF1D1D27),
        c700 = Color(0xFF26262F),
        c600 = Color(0xFF6E6E79),
        c500 = Color(0xFF84848F),
        c400 = Color(0xFF9C9CA5),
        c300 = Color(0xFFB3B3BA),
        c200 = Color(0xFFCBCBCF),
        c100 = Color(0xFFE8E8EA),
        cMax = Color.White,
        line = Color(0xFF1E1E28),
        hlPale = Color(0xFFDDD6FE),
        hlBright = Color(0xFFA78BFA),
        hlMid = Color(0xFF7C3AED),
        hlDim = Color(0xFF5B21B6),
        hlInk = Color.White,
        edgeSoft = white(0.04f),
        edge = white(0.06f),
        edgeStrong = white(0.12f),
        fillHover = white(0.05f),
        fillActive = white(0.09f),
        toneDarken = 0f,
        serifDisplay = false,
    )

    val Paper = Palette(
        isLight = true,
        c950 = Color(0xFFF4EFE4),
        c900 = Color(0xFFFDFAF3),
        c850 = Color(0xFFF6F1E6),
        c800 = Color(0xFFF0EBDF),
        c750 = Color(0xFFE8E1D2),
        c700 = Color(0xFFDCD4C2),
        c600 = Color(0xFF887A6B),
        c500 = Color(0xFF716458),
        c400 = Color(0xFF5B5148),
        c300 = Color(0xFF484039),
        c200 = Color(0xFF352F2A),
        c100 = Color(0xFF1E1A17),
        cMax = Color(0xFF14100C),
        line = Color(0xFFE3DBC9),
        hlPale = Color(0xFFA03A44),
        hlBright = Color(0xFF8C2F39),
        hlMid = Color(0xFF7D2A33),
        hlDim = Color(0xFF6F242C),
        hlInk = Color(0xFFFDF8F2),
        edgeSoft = umber(0.06f),
        edge = umber(0.10f),
        edgeStrong = umber(0.18f),
        fillHover = umber(0.05f),
        fillActive = umber(0.10f),
        toneDarken = 0.62f,
        serifDisplay = true,
    )

    val Hearth = Palette(
        isLight = false,
        c950 = Color(0xFF0B0907),
        c900 = Color(0xFF130F0B),
        c850 = Color(0xFF17120E),
        c800 = Color(0xFF1B1510),
        c750 = Color(0xFF1F1813),
        c700 = Color(0xFF231C16),
        c600 = Color(0xFF856851),
        c500 = Color(0xFF9F7D62),
        c400 = Color(0xFFB19681),
        c300 = Color(0xFFC4AE9E),
        c200 = Color(0xFFD6C8BC),
        c100 = Color(0xFFECE5DF),
        cMax = Color(0xFFFFFAF4),
        line = Color(0xFF2E241C),
        hlPale = Color(0xFFFFD9A8),
        hlBright = Color(0xFFF0A868),
        hlMid = Color(0xFFC9722F),
        hlDim = Color(0xFF8A4A1C),
        hlInk = Color(0xFF1A1005),
        edgeSoft = warm(0.045f),
        edge = warm(0.07f),
        edgeStrong = warm(0.14f),
        fillHover = warm(0.05f),
        fillActive = warm(0.09f),
        toneDarken = 0f,
        serifDisplay = true,
    )
}

/**
 * Status and achievement-tier hues carry meaning, so no theme recolours them
 * — the same invariant the web enforces. A light ground only darkens their
 * ink ([Palette.toneInk]); the hue stays.
 */
object Tones {
    val Backlog = Color(0xFF94A3B8)
    val Playing = Color(0xFF22D3EE)
    val Played = Color(0xFF34D399)
    val Dropped = Color(0xFFF87171)

    val Bronze = Color(0xFFD9A06B)
    val Silver = Color(0xFFC4CCD8)
    val Gold = Color(0xFFE6C35C)
    val Legendary = Color(0xFF82E6FF)
}
