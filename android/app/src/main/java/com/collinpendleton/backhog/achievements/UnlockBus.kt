package com.collinpendleton.backhog.achievements

import com.collinpendleton.backhog.api.AchievementStatus
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update

/**
 * The app's toast bus — the Android stand-in for the web's `backhog:unlocks`
 * window event. Mutations carry their unlocks home (`patchEntry`, `addSession`,
 * `egg`); the ViewModels hand them here and the toast stack mounted once in the
 * Shell celebrates. Toasts self-expire from the queue; the UI only reads.
 */
class UnlockBus {
    private val _toasts = MutableStateFlow<List<AchievementStatus>>(emptyList())
    val toasts: StateFlow<List<AchievementStatus>> = _toasts.asStateFlow()

    fun unlock(unlocks: List<AchievementStatus>) {
        if (unlocks.isEmpty()) return
        _toasts.update { it + unlocks }
    }

    fun unlock(unlock: AchievementStatus?) {
        unlock?.let { unlock(listOf(it)) }
    }

    /** Dismiss the oldest toast — what the auto-dismiss timer calls. */
    fun dismissOldest() {
        _toasts.update { it.drop(1) }
    }

    fun clear() {
        _toasts.value = emptyList()
    }
}
