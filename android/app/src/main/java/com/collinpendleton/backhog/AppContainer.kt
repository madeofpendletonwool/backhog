package com.collinpendleton.backhog

import android.content.Context
import com.collinpendleton.backhog.achievements.UnlockBus
import com.collinpendleton.backhog.api.PersistentCookieJar
import com.collinpendleton.backhog.api.SharedPrefsCookieStore
import com.collinpendleton.backhog.data.AppPreferences
import com.collinpendleton.backhog.player.PlayerConnection
import com.collinpendleton.backhog.session.SessionManager
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
    val session = SessionManager(preferences, cookieJar, appScope)

    /**
     * The streaming client: same cookie jar as the API, no interceptors of
     * its own — ExoPlayer pulls track bytes and covers through it, so the
     * player's requests ride the same session the rest of the app does.
     */
    val mediaClient: OkHttpClient = OkHttpClient.Builder()
        .cookieJar(cookieJar)
        .connectTimeout(15, TimeUnit.SECONDS)
        .readTimeout(30, TimeUnit.SECONDS)
        .build()

    /** The app's handle on the playback service: its controller, its state, its commands. */
    val player = PlayerConnection(context, this)
}
