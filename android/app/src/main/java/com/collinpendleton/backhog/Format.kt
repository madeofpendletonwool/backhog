package com.collinpendleton.backhog

import java.time.Instant
import java.time.LocalDate
import java.time.LocalDateTime
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale

/**
 * The formatting rules the web renders with (web/src/lib/format.ts), ported so
 * the app says exactly what the web says. Pure functions, JVM-testable.
 */
object Format {
    /** A compact "12h" / "1h 30m" / "45m" for a seconds duration. */
    fun duration(seconds: Long?): String {
        if (seconds == null || seconds <= 0) return "—"
        val hours = seconds / 3600
        val minutes = Math.round((seconds % 3600) / 60.0)
        return when {
            hours == 0L -> "${minutes}m"
            minutes == 0L -> "${hours}h"
            else -> "${hours}h ${minutes}m"
        }
    }

    /** Whole-hour totals: one decimal under ten hours, rounded above ("8.5h" / "23h"). */
    fun hours(hours: Double): String = when {
        hours <= 0 -> "0h"
        hours < 10 -> String.format(Locale.ROOT, "%.1fh", hours)
        else -> "${Math.round(hours)}h"
    }

    /** "1h 30m" from a minutes count — the session log's unit. */
    fun minutes(minutes: Int): String {
        val hours = minutes / 60
        val rest = minutes % 60
        return when {
            hours == 0 -> "${rest}m"
            rest == 0 -> "${hours}h"
            else -> "${hours}h ${rest}m"
        }
    }

    /** IGDB stores release dates as unix seconds. */
    fun releaseYear(unixSeconds: Long?): String =
        unixSeconds?.let { utcYearOf(it).toString() } ?: ""

    /** "Feb 25, 2022" — the detail page's full release date. */
    fun releaseDate(unixSeconds: Long?, zone: ZoneId = ZoneId.systemDefault()): String =
        unixSeconds?.let {
            Instant.ofEpochSecond(it).atZone(zone).toLocalDate()
                .format(DateTimeFormatter.ofPattern("MMM d, yyyy", Locale.getDefault()))
        } ?: ""

    /** Renders a stored `2026-07-20` without the timezone shifts of parsing it as an instant. */
    fun playedOn(isoDate: String): String {
        val parts = isoDate.split("-").mapNotNull { it.toIntOrNull() }
        if (parts.size != 3 || parts[0] == 0 || parts[1] == 0 || parts[2] == 0) return isoDate
        val date = LocalDate.of(parts[0], parts[1], parts[2])
        return date.format(DateTimeFormatter.ofPattern("MMM d, yyyy", Locale.getDefault()))
    }

    /** An RFC3339 timestamp → "Feb 25, 2026"; "—" when absent. */
    fun date(iso: String?): String {
        if (iso.isNullOrBlank()) return "—"
        return runCatching {
            Instant.parse(iso).atZone(ZoneId.systemDefault()).toLocalDate()
                .format(DateTimeFormatter.ofPattern("MMM d, yyyy", Locale.getDefault()))
        }.getOrElse { isoDate(iso) ?: "—" }
    }

    /** Today as `YYYY-MM-DD` in the local zone — the session form's default and max. */
    fun today(zone: ZoneId = ZoneId.systemDefault()): String = LocalDate.now(zone).toString()

    /**
     * "3 days ago" / "2 months ago" — stalled/aging hints. `now` is injectable
     * so the tests don't depend on the wall clock.
     */
    fun relativeTime(iso: String?, now: Long = System.currentTimeMillis()): String {
        if (iso.isNullOrBlank()) return ""
        val then = runCatching { Instant.parse(iso).toEpochMilli() }.getOrNull() ?: return ""
        val days = ((now - then) / 86_400_000L).toInt()
        return when {
            days < 1 -> "today"
            days == 1 -> "yesterday"
            days < 30 -> "$days days ago"
            else -> {
                val months = days / 30
                if (months < 12) "$months month${if (months == 1) "" else "s"} ago"
                else {
                    val years = months / 12
                    "$years year${if (years == 1) "" else "s"} ago"
                }
            }
        }
    }

    private fun utcYearOf(unixSeconds: Long): Int =
        LocalDateTime.ofInstant(Instant.ofEpochSecond(unixSeconds), ZoneId.of("UTC")).year

    private fun isoDate(raw: String): String? = runCatching {
        LocalDate.parse(raw.take(10))
            .format(DateTimeFormatter.ofPattern("MMM d, yyyy", Locale.getDefault()))
    }.getOrNull()

    // --- IGDB CDN ------------------------------------------------------

    private const val IGDB_IMG = "https://images.igdb.com/igdb/image/upload"

    fun igdbImage(imageId: String, size: String): String = "$IGDB_IMG/t_$size/$imageId.jpg"

    fun screenshotThumb(imageId: String): String = igdbImage(imageId, "screenshot_med")

    fun screenshotFull(imageId: String): String = igdbImage(imageId, "screenshot_huge")

    fun relatedCover(imageId: String): String = igdbImage(imageId, "cover_small_2x")

    // --- website labels ------------------------------------------------

    private val WEBSITE_RULES: List<Pair<Regex, String>> = listOf(
        Regex("nintendo") to "Nintendo",
        Regex("playstation") to "PlayStation Store",
        Regex("steampowered|steamcommunity") to "Steam",
        Regex("xbox") to "Xbox",
        Regex("epicgames") to "Epic Games",
        Regex("gog\\.com") to "GOG",
        Regex("microsoft") to "Microsoft Store",
        Regex("apps\\.apple|itunes\\.apple") to "App Store",
        Regex("play\\.google") to "Google Play",
        Regex("twitch") to "Twitch",
        Regex("youtube|youtu\\.be") to "YouTube",
        Regex("twitter|(^|\\.)x\\.com") to "Twitter / X",
        Regex("facebook") to "Facebook",
        Regex("instagram") to "Instagram",
        Regex("discord") to "Discord",
        Regex("reddit") to "Reddit",
        Regex("wikipedia") to "Wikipedia",
        Regex("fandom|wikia") to "Wiki",
        Regex("itch\\.io") to "itch.io",
    )

    /** A human label for an external link, derived from its host; the bare domain as fallback. */
    fun websiteLabel(url: String): String {
        val host = runCatching {
            java.net.URI(url).host?.removePrefix("www.")?.lowercase(Locale.ROOT)
        }.getOrNull() ?: return "Website"
        if (host.isBlank()) return "Website"
        WEBSITE_RULES.firstOrNull { it.first.containsMatchIn(host) }?.let { return it.second }
        return host
    }
}
