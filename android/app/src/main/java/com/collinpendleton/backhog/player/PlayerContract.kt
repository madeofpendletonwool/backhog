package com.collinpendleton.backhog.player

/**
 * The wire between the app UI and the playback service: the custom session
 * commands the UI sends, and the session extras the service publishes. One
 * file, because both sides must agree on every string in it.
 */
object PlayerContract {
    const val CMD_OPEN = "com.collinpendleton.backhog.player.OPEN"
    const val CMD_CLOSE = "com.collinpendleton.backhog.player.CLOSE"
    const val CMD_RELOAD = "com.collinpendleton.backhog.player.RELOAD"
    const val CMD_SLEEP = "com.collinpendleton.backhog.player.SLEEP"

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
    const val EXTRA_ENTRY_ID = "com.collinpendleton.backhog.player.ENTRY_ID"
    const val EXTRA_TITLE = "com.collinpendleton.backhog.player.TITLE"
    const val EXTRA_AUTHORS = "com.collinpendleton.backhog.player.AUTHORS"
    const val EXTRA_COVER = "com.collinpendleton.backhog.player.COVER"
    const val EXTRA_ACCENT = "com.collinpendleton.backhog.player.ACCENT"
    const val EXTRA_TIMELINE = "com.collinpendleton.backhog.player.TIMELINE"
    const val EXTRA_DEGRADED = "com.collinpendleton.backhog.player.DEGRADED"
    const val EXTRA_ERROR = "com.collinpendleton.backhog.player.ERROR"
    const val EXTRA_SLEEP_KIND = "com.collinpendleton.backhog.player.SLEEP_KIND"
    const val EXTRA_SLEEP_MINUTES = "com.collinpendleton.backhog.player.SLEEP_MINUTES"
    const val EXTRA_SLEEP_ENDS_AT = "com.collinpendleton.backhog.player.SLEEP_ENDS_AT"
}
