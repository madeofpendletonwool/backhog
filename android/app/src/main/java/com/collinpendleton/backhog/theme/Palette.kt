package com.collinpendleton.backhog.theme

import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp

/**
 * The palette ladders ported from the web's token system
 * (web/src/themes/flat.css, web/src/themes/library.css, web/src/index.css).
 *
 * The contract the web enforces travels with the values:
 *
 * 1. The ink ramp is a fixed *contrast* ladder. Roles never move: 950 is the
 *    page, 800 the plate text is solved against, 100 the strongest ink.
 * 2. Meaning-bearing colour (status hues, achievement tiers) is constant in
 *    hue in every theme; on the light ground the ink is pulled toward c-100
 *    by [toneDarken] (0 on every dark ground, 62% on Paper).
 * 3. hl-* is the "this is the live one" ramp — brand violet in Midnight,
 *    ember or oxblood in the library. It is chrome, so it re-themes.
 */
data class BackhogPalette(
    // The ink ladder.
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
    /** The strongest ink there is — white on dark grounds, near-black on light. */
    val cMax: Color,
    val cLine: Color,

    // The "a bit lighter than what's behind me" set. Light grounds use ink
    // alphas instead of light ones.
    val edgeSoft: Color,
    val edge: Color,
    val edgeStrong: Color,
    val fillHover: Color,
    val fillActive: Color,
    val outline: Color,
    val outlineStrong: Color,

    // The highlight ramp and the primary action built on it.
    val hlPale: Color,
    val hlBright: Color,
    val hlMid: Color,
    val hlDim: Color,
    val primaryBg: Color,
    val primaryInk: Color,

    // Danger, a printer's red in the library.
    val dangerBg: Color,
    val dangerInk: Color,

    val isLight: Boolean,
    /** How far status/tier inks are pulled toward c-100. 0 on dark grounds. */
    val toneDarken: Float,

    // Family chrome: print corners versus rounded plastic, serif versus sans.
    val radiusPanel: Dp,
    val radiusField: Dp,
    val displayFont: FontFamily,
) {
    companion object {
        /** Midnight — the flat family, from flat.css and index.css :root. */
        val MIDNIGHT =
            BackhogPalette(
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
                cMax = Color(0xFFFFFFFF),
                cLine = Color(0xFF1E1E28),
                edgeSoft = Color(0x0AFFFFFF),
                edge = Color(0x0FFFFFFF),
                edgeStrong = Color(0x1FFFFFFF),
                fillHover = Color(0x0DFFFFFF),
                fillActive = Color(0x17FFFFFF),
                outline = Color(0x33FFFFFF),
                outlineStrong = Color(0x66FFFFFF),
                hlPale = Color(0xFFDDD6FE),
                hlBright = Color(0xFFA78BFA),
                hlMid = Color(0xFF7C3AED),
                hlDim = Color(0xFF5B21B6),
                // Brand violet on white text — the palette's best pair.
                primaryBg = Color(0xFF7C3AED),
                primaryInk = Color(0xFFFFFFFF),
                dangerBg = Color(0xFFDC2626),
                dangerInk = Color(0xFFFFFFFF),
                isLight = false,
                toneDarken = 0f,
                radiusPanel = 18.dp,
                radiusField = 14.dp,
                displayFont = FontFamily.SansSerif,
            )

        /** Hearth — the library family's dark theme, a room lit by a fire. */
        val HEARTH =
            BackhogPalette(
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
                cLine = Color(0xFF2E241C),
                // Warm light falls on warm surfaces: firelight hairlines.
                edgeSoft = Color(0x0BFFE9CD),
                edge = Color(0x12FFE9CD),
                edgeStrong = Color(0x24FFE9CD),
                fillHover = Color(0x0DFFE9CD),
                fillActive = Color(0x17FFE9CD),
                outline = Color(0x38FFE9CD),
                outlineStrong = Color(0x6BFFE9CD),
                // Ember — the warmest thing on the page.
                hlPale = Color(0xFFFFD9A8),
                hlBright = Color(0xFFF0A868),
                hlMid = Color(0xFFC9722F),
                hlDim = Color(0xFF8A4A1C),
                primaryBg = Color(0xFFF0A868),
                primaryInk = Color(0xFF1A1005),
                dangerBg = Color(0xFFA33236),
                dangerInk = Color(0xFFFFFFFF),
                isLight = false,
                toneDarken = 0f,
                radiusPanel = 6.dp,
                radiusField = 4.dp,
                displayFont = FontFamily.Serif,
            )

        /** Paper — the one light ground, warm daylight. */
        val PAPER =
            BackhogPalette(
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
                cLine = Color(0xFFE3DBC9),
                // Ink alphas, not light ones — the whole point of the ground.
                edgeSoft = Color(0x0F3A2C1A),
                edge = Color(0x1A3A2C1A),
                edgeStrong = Color(0x2E3A2C1A),
                fillHover = Color(0x0D3A2C1A),
                fillActive = Color(0x1A3A2C1A),
                outline = Color(0x473A2C1A),
                outlineStrong = Color(0x803A2C1A),
                // Oxblood: a printer's red, readable as text and as a fill.
                hlPale = Color(0xFFA03A44),
                hlBright = Color(0xFF8C2F39),
                hlMid = Color(0xFF7D2A33),
                hlDim = Color(0xFF6F242C),
                primaryBg = Color(0xFF8C2F39),
                primaryInk = Color(0xFFFDF8F2),
                dangerBg = Color(0xFFA33236),
                dangerInk = Color(0xFFFFFFFF),
                isLight = true,
                toneDarken = 0.62f,
                radiusPanel = 6.dp,
                radiusField = 4.dp,
                displayFont = FontFamily.Serif,
            )
    }
}

/** The web's eight-digit hex alphas are levels of a 255 scale; Compose takes
 *  an alpha channel in the ARGB long, which the Color(0xAARRGGBB) literals
 *  above encode directly. */

fun paletteOf(theme: BackhogTheme): BackhogPalette =
    when (theme) {
        BackhogTheme.MIDNIGHT -> BackhogPalette.MIDNIGHT
        BackhogTheme.PAPER -> BackhogPalette.PAPER
        BackhogTheme.HEARTH -> BackhogPalette.HEARTH
    }
