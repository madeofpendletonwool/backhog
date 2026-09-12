package com.collinpendleton.backhog.api

import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.SharedFlow

/**
 * The single "your session stopped being valid" signal. Any response body may
 * emit it; the session manager is the only listener, so there is exactly one
 * place the app decides what a dead session means.
 */
object SessionEvents {
    val unauthorized = MutableSharedFlow<Unit>(extraBufferCapacity = 1)

    /** Emitted by the 401 interceptor for routes outside the auth family. */
    val expired: SharedFlow<Unit> = unauthorized
}
