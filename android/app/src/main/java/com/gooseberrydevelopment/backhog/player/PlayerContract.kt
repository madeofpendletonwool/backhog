package com.gooseberrydevelopment.backhog.player

/**
 * The wire between the app UI and the playback service: the custom session
 * commands the UI sends, and the session extras the service publishes. One
 * file, because both sides must agree on every string in it.
 */
object PlayerContract {
    const val CMD_OPEN = "com.gooseberrydevelopment.backhog.player.OPEN"
    const val CMD_CLOSE = "com.gooseberrydevelopment.backhog.player.CLOSE"
    const val CMD_RELOAD = "com.gooseberrydevelopment.backhog.player.RELOAD"
    const val CMD_SLEEP = "com.gooseberrydevelopment.backhog.player.SLEEP"

    val COMMANDS = listOf(CMD_OPEN, CMD_CLOSE, CMD_RELOAD, CMD_SLEEP)

    /** [CMD_OPEN] arguments. */
    const val ARG_ENTRY_ID = "entry_id"
    /** A global second to land on (a handoff); absent means "the server's position". */
    const val ARG_START_AT = "start_at"
    const val ARG_AUTOPLAY = "autoplay"

    /** [CMD_SLEEP] arguments. */
    const val ARG_KIND = "kind"
    const val ARG_MINUTES = "minutes"

    const val SLEEP_OFF = "off"
    const val SLEEP_MINUTES = "minutes"
    const val SLEEP_CHAPTER = "chapter"

    /** Session extras — the book-level picture; the clock and transport are the player's own. */
    const val EXTRA_ENTRY_ID = "com.gooseberrydevelopment.backhog.player.ENTRY_ID"
    const val EXTRA_TITLE = "com.gooseberrydevelopment.backhog.player.TITLE"
    const val EXTRA_AUTHORS = "com.gooseberrydevelopment.backhog.player.AUTHORS"
    const val EXTRA_COVER = "com.gooseberrydevelopment.backhog.player.COVER"
    const val EXTRA_ACCENT = "com.gooseberrydevelopment.backhog.player.ACCENT"
    const val EXTRA_TIMELINE = "com.gooseberrydevelopment.backhog.player.TIMELINE"
    const val EXTRA_DEGRADED = "com.gooseberrydevelopment.backhog.player.DEGRADED"
    const val EXTRA_ERROR = "com.gooseberrydevelopment.backhog.player.ERROR"
    const val EXTRA_SLEEP_KIND = "com.gooseberrydevelopment.backhog.player.SLEEP_KIND"
    const val EXTRA_SLEEP_MINUTES = "com.gooseberrydevelopment.backhog.player.SLEEP_MINUTES"
    const val EXTRA_SLEEP_ENDS_AT = "com.gooseberrydevelopment.backhog.player.SLEEP_ENDS_AT"
}
