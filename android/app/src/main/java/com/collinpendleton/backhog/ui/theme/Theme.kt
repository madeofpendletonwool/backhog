package com.collinpendleton.backhog.ui.theme

import androidx.compose.material3.ColorScheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Shapes
import androidx.compose.material3.Typography
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp

val LocalPalette = staticCompositionLocalOf { Palettes.Midnight }
val LocalThemeId = staticCompositionLocalOf { ThemeId.Default }

/** The raw token ladder, for anything Material's roles do not name. */
object Backhog {
    val palette: Palette
        @Composable get() = LocalPalette.current
}

private fun Palette.colorScheme(): ColorScheme {
    val base = if (isLight) lightColorScheme() else darkColorScheme()
    // The flat family's button is the violet --hl-mid; library themes use --hl-bright.
    val primary = if (serifDisplay) hlBright else hlMid
    return base.copy(
        primary = primary,
        onPrimary = hlInk,
        primaryContainer = hlDim,
        onPrimaryContainer = hlPale,
        inversePrimary = hlPale,
        secondary = hlBright,
        onSecondary = hlInk,
        secondaryContainer = fillActive.compositeOn(c850),
        onSecondaryContainer = c100,
        tertiary = hlPale,
        onTertiary = hlInk,
        background = c950,
        onBackground = c100,
        surface = c900,
        onSurface = c100,
        surfaceVariant = c800,
        onSurfaceVariant = c400,
        surfaceTint = Color.Transparent,
        surfaceDim = c950,
        surfaceBright = c750,
        surfaceContainerLowest = c950,
        surfaceContainerLow = c900,
        surfaceContainer = c850,
        surfaceContainerHigh = c800,
        surfaceContainerHighest = c750,
        inverseSurface = c100,
        inverseOnSurface = c900,
        outline = edgeStrong.compositeOn(c900),
        outlineVariant = edge.compositeOn(c900),
        error = toneInk(Tones.Dropped),
        onError = if (isLight) c900 else c950,
        errorContainer = Tones.Dropped.copy(alpha = 0.15f).compositeOn(c900),
        onErrorContainer = toneInk(Tones.Dropped),
        scrim = Color.Black.copy(alpha = 0.6f),
    )
}

private fun Color.compositeOn(ground: Color): Color {
    val a = alpha
    return Color(
        red = red * a + ground.red * (1 - a),
        green = green * a + ground.green * (1 - a),
        blue = blue * a + ground.blue * (1 - a),
        alpha = 1f,
    )
}

private fun Palette.typography(): Typography {
    val base = Typography()
    if (!serifDisplay) return base
    // The library family sets its display and headings in a serif; body text stays sans for UI legibility.
    return base.copy(
        displayLarge = base.displayLarge.copy(fontFamily = FontFamily.Serif),
        displayMedium = base.displayMedium.copy(fontFamily = FontFamily.Serif),
        displaySmall = base.displaySmall.copy(fontFamily = FontFamily.Serif),
        headlineLarge = base.headlineLarge.copy(fontFamily = FontFamily.Serif),
        headlineMedium = base.headlineMedium.copy(fontFamily = FontFamily.Serif),
        headlineSmall = base.headlineSmall.copy(fontFamily = FontFamily.Serif),
        titleLarge = base.titleLarge.copy(fontFamily = FontFamily.Serif),
    )
}

private fun Palette.shapes(): Shapes =
    if (serifDisplay) {
        // Print-flat: the library draws rules, not pills.
        Shapes(
            extraSmall = RoundedCornerShape(2.dp),
            small = RoundedCornerShape(3.dp),
            medium = RoundedCornerShape(4.dp),
            large = RoundedCornerShape(6.dp),
            extraLarge = RoundedCornerShape(8.dp),
        )
    } else {
        // --r-xl / --r-2xl: 14 and 18px.
        Shapes(
            extraSmall = RoundedCornerShape(6.dp),
            small = RoundedCornerShape(10.dp),
            medium = RoundedCornerShape(14.dp),
            large = RoundedCornerShape(18.dp),
            extraLarge = RoundedCornerShape(24.dp),
        )
    }

@Composable
fun BackhogTheme(theme: ThemeId = ThemeId.Default, content: @Composable () -> Unit) {
    val palette = theme.palette
    CompositionLocalProvider(LocalPalette provides palette, LocalThemeId provides theme) {
        MaterialTheme(
            colorScheme = palette.colorScheme(),
            typography = palette.typography(),
            shapes = palette.shapes(),
            content = content,
        )
    }
}
