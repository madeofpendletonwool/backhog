package com.gooseberrydevelopment.backhog

import android.content.Context
import com.gooseberrydevelopment.backhog.achievements.UnlockBus
import com.gooseberrydevelopment.backhog.api.PersistentCookieJar
import com.gooseberrydevelopment.backhog.api.SharedPrefsCookieStore
import com.gooseberrydevelopment.backhog.data.AppPreferences
import com.gooseberrydevelopment.backhog.player.PlayerConnection
import com.gooseberrydevelopment.backhog.session.SessionManager
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import okhttp3.OkHttpClient
import java.util.concurrent.TimeUnit

/** Hand-rolled DI: the app is small enough that a graph of three objects does not need a framework. */
class AppContainer(context: Context) {
    val appScope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    val preferences = AppPreferences(context)
    val unlocks = UnlockBus()
    val cookieJar = PersistentCookieJar(
        SharedPrefsCookieStore(context.getSharedPreferences("backhog_cookies", Context.MODE_PRIVATE)),
    )

    /**
     * The app's one HTTP client and connection pool. ExoPlayer streams track
     * bytes through it as is, Coil loads covers through it, and the API
     * builds on it with its own interceptors — all on the same session
     * cookie and the same warm connections to the server.
     */
    val httpClient: OkHttpClient = OkHttpClient.Builder()
        .cookieJar(cookieJar)
        .connectTimeout(15, TimeUnit.SECONDS)
        .readTimeout(30, TimeUnit.SECONDS)
        .build()

    val session = SessionManager(preferences, cookieJar, appScope, httpClient)

    /** The app's handle on the playback service: its controller, its state, its commands. */
    val player = PlayerConnection(context, this)
}
