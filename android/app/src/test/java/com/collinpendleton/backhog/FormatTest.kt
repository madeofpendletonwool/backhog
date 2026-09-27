package com.collinpendleton.backhog

import org.junit.Assert.assertEquals
import org.junit.Test
import java.time.ZoneId

class FormatTest {
    @Test fun `durations are compact`() {
        assertEquals("—", Format.duration(null))
        assertEquals("—", Format.duration(0))
        assertEquals("45m", Format.duration(45 * 60))
        assertEquals("12h", Format.duration(12 * 3600))
        assertEquals("1h 30m", Format.duration(5400))
    }

    @Test fun `hour totals show one decimal under ten and round above`() {
        assertEquals("0h", Format.hours(0.0))
        assertEquals("8.5h", Format.hours(8.5))
        assertEquals("23h", Format.hours(22.6))
    }

    @Test fun `minutes format like the session log`() {
        assertEquals("45m", Format.minutes(45))
        assertEquals("2h", Format.minutes(120))
        assertEquals("1h 30m", Format.minutes(90))
    }

    @Test fun `release years are utc`() {
        // 2022-03-25T00:30:00Z is still March 24 in America/Los_Angeles.
        assertEquals("2022", Format.releaseYear(1648168200L))
        assertEquals("", Format.releaseYear(null))
    }

    @Test fun `release dates render month day year`() {
        assertEquals("", Format.releaseDate(null))
        val tokyo = Format.releaseDate(1711333200L, ZoneId.of("Asia/Tokyo")) // 2024-03-25 08:00 JST
        assertEquals(true, tokyo.endsWith("2024"))
        assertEquals(true, tokyo.contains("25"))
    }

    @Test fun `played_on parses without timezone drift`() {
        assertEquals("Jul 20, 2026", Format.playedOn("2026-07-20"))
        // Garbage passes through untouched rather than throwing.
        assertEquals("sometime", Format.playedOn("sometime"))
    }

    @Test fun `timestamps render dates and dashes`() {
        assertEquals("—", Format.date(null))
        val text = Format.date("2026-02-25T13:00:00Z")
        assertEquals("2026", text.substringAfter(", "))
    }

    @Test fun `relative time buckets like the web`() {
        val now = 1_800_000_000_000L
        assertEquals("", Format.relativeTime(null, now))
        assertEquals("today", Format.relativeTime(java.time.Instant.ofEpochMilli(now - 3_600_000).toString(), now))
        assertEquals("yesterday", Format.relativeTime(java.time.Instant.ofEpochMilli(now - 86_400_000).toString(), now))
        assertEquals("5 days ago", Format.relativeTime(java.time.Instant.ofEpochMilli(now - 5 * 86_400_000).toString(), now))
        assertEquals("2 months ago", Format.relativeTime(java.time.Instant.ofEpochMilli(now - 65L * 86_400_000).toString(), now))
        assertEquals("1 year ago", Format.relativeTime(java.time.Instant.ofEpochMilli(now - 370L * 86_400_000).toString(), now))
    }

    @Test fun `website labels come from the host`() {
        assertEquals("Steam", Format.websiteLabel("https://store.steampowered.com/app/570/"))
        assertEquals("Nintendo", Format.websiteLabel("https://www.nintendo.com/store/"))
        assertEquals("YouTube", Format.websiteLabel("https://youtu.be/xyz"))
        assertEquals("eldenring.com", Format.websiteLabel("https://eldenring.com/page"))
        assertEquals("Website", Format.websiteLabel("not a url"))
    }

    @Test fun `igdb urls use the named size presets`() {
        assertEquals(
            "https://images.igdb.com/igdb/image/upload/t_screenshot_med/abc1.jpg",
            Format.screenshotThumb("abc1"),
        )
        assertEquals(
            "https://images.igdb.com/igdb/image/upload/t_cover_small_2x/abc1.jpg",
            Format.relatedCover("abc1"),
        )
    }

    @Test fun `today is an iso date`() {
        assertEquals(true, Regex("""\d{4}-\d{2}-\d{2}""").matches(Format.today()))
    }
}
