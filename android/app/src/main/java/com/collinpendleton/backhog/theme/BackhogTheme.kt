package com.collinpendleton.backhog.theme

import androidx.compose.material3.ColorScheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.staticCompositionLocalOf

/**
 * The live palette. Components read tokens from here the way the web reads
 * CSS custom properties — nothing below the theme boundary names a theme.
 */
val LocalBackhogPalette = staticCompositionLocalOf { BackhogPalette.MIDNIGHT }

private fun colorScheme(p: BackhogPalette): ColorScheme =
    if (p.isLight) {
        lightColorScheme(
            primary = p.primaryBg,
            onPrimary = p.primaryInk,
            secondary = p.hlMid,
            onSecondary = p.primaryInk,
            error = p.dangerBg,
            onError = p.dangerInk,
            background = p.c950,
            onBackground = p.c100,
            surface = p.c900,
            onSurface = p.c100,
            surfaceVariant = p.c800,
            onSurfaceVariant = p.c400,
            outline = p.outlineStrong,
            outlineVariant = p.edge,
        )
    } else {
        darkColorScheme(
            primary = p.primaryBg,
            onPrimary = p.primaryInk,
            secondary = p.hlMid,
            onSecondary = p.primaryInk,
            error = p.dangerBg,
            onError = p.dangerInk,
            background = p.c950,
            onBackground = p.c100,
            surface = p.c900,
            onSurface = p.c100,
            surfaceVariant = p.c800,
            onSurfaceVariant = p.c400,
            outline = p.outlineStrong,
            outlineVariant = p.edge,
        )
    }

/**
 * Dress the app in one Backhog theme. The app is always in *some* Backhog
 * palette — there is no un-themed state — so the system light/dark setting
 * does not apply; the theme is the reader's explicit choice, per arena.
 */
@Composable
fun BackhogTheme(theme: BackhogTheme, content: @Composable () -> Unit) {
    val palette = paletteOf(theme)
    MaterialTheme(colorScheme = colorScheme(palette)) {
        CompositionLocalProvider(LocalBackhogPalette provides palette, content = content)
    }
}
