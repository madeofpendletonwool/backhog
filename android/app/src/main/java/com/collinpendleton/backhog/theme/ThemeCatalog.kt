package com.collinpendleton.backhog.theme

/**
 * The theme catalogue — data only, mirroring web/src/lib/themes.ts.
 *
 * A *family* decides how the chrome is built; a *theme* is one palette
 * inside a family. The Android port ships two of the web's three families:
 * flat (Midnight) and library (Paper / Hearth). The arcade pixel family is
 * deferred to a later pass by scope decision.
 */

enum class ThemeFamily(val label: String, val blurb: String) {
    /** Quiet dark chrome. Rounded surfaces, one violet accent. */
    FLAT("Midnight", "Quiet dark chrome. Rounded surfaces, one violet accent, nothing between you and the covers."),
    LIBRARY("Library", "Ink on paper. Serif type, rules instead of boxes, and the page you are on marked in the margin."),
}

/**
 * The library family's second tier — two grounds to read on. Flat has a
 * single theme, so its tier is null and the picker shows no row.
 */
val TIER_LABEL: Map<ThemeFamily, Pair<String, String>?> =
    mapOf(
        ThemeFamily.FLAT to null,
        ThemeFamily.LIBRARY to ("Light" to "Read by daylight or by the fire. Same room, different hour."),
    )

enum class BackhogTheme(
    val label: String,
    val note: String,
    val family: ThemeFamily,
) {
    MIDNIGHT("Midnight", "The original dark", ThemeFamily.FLAT),
    PAPER("Paper", "Warm daylight", ThemeFamily.LIBRARY),
    HEARTH("Hearth", "Read by the fire", ThemeFamily.LIBRARY),
}

/** Picker order, the web's THEMES order restricted to the shipped families. */
val Themes = BackhogTheme.entries

val DEFAULT_THEME = BackhogTheme.MIDNIGHT

fun familyOf(theme: BackhogTheme): ThemeFamily = theme.family

/** Every theme in a family, in picker order. */
fun themesInFamily(family: ThemeFamily): List<BackhogTheme> = Themes.filter { it.family == family }

/** One slot per arena — Backhog is two arenas and each keeps its own theme. */
data class ThemeSlots(
    val games: BackhogTheme = DEFAULT_THEME,
    val books: BackhogTheme = DEFAULT_THEME,
)
