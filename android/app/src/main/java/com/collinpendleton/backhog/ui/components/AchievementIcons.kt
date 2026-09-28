package com.collinpendleton.backhog.ui.components

import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Bedtime
import androidx.compose.material.icons.filled.EmojiEvents
import androidx.compose.material.icons.filled.Extension
import androidx.compose.material.icons.filled.Flag
import androidx.compose.material.icons.filled.HourglassEmpty
import androidx.compose.material.icons.filled.MilitaryTech
import androidx.compose.material.icons.filled.NightsStay
import androidx.compose.material.icons.filled.QueryStats
import androidx.compose.material.icons.filled.Shuffle
import androidx.compose.material.icons.filled.Speed
import androidx.compose.material.icons.filled.Star
import androidx.compose.material.icons.filled.Timer
import androidx.compose.material.icons.filled.Visibility
import androidx.compose.material.icons.filled.WorkspacePremium
import androidx.compose.ui.graphics.vector.ImageVector

/**
 * The icon code keys the server serves, mapped to Material glyphs — the port
 * of the web's achievementIcons.ts. The masked icon ("hidden") lands on a
 * question mark; unknown keys fall back to the trophy like the web does.
 */
fun achievementIcon(icon: String): ImageVector = when (icon) {
    "trophy" -> Icons.Filled.EmojiEvents
    "medal" -> Icons.Filled.MilitaryTech
    "crown" -> Icons.Filled.WorkspacePremium
    "star" -> Icons.Filled.Star
    "timer", "clock" -> Icons.Filled.Timer
    "stopwatch", "speed" -> Icons.Filled.Speed
    "hourglass" -> Icons.Filled.HourglassEmpty
    "flag", "finish" -> Icons.Filled.Flag
    "stats", "chart" -> Icons.Filled.QueryStats
    "night-sleep" -> Icons.Filled.Bedtime
    "moon" -> Icons.Filled.NightsStay
    "eyeball" -> Icons.Filled.Visibility
    "whirlwind", "shuffle" -> Icons.Filled.Shuffle
    "puzzle" -> Icons.Filled.Extension
    else -> Icons.Filled.EmojiEvents
}
