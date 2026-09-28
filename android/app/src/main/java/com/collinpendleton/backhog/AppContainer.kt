package com.collinpendleton.backhog

import android.content.Context
import com.collinpendleton.backhog.achievements.UnlockBus
import com.collinpendleton.backhog.api.PersistentCookieJar
import com.collinpendleton.backhog.api.SharedPrefsCookieStore
import com.collinpendleton.backhog.data.AppPreferences
import com.collinpendleton.backhog.session.SessionManager
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob

/** Hand-rolled DI: the app is small enough that a graph of three objects does not need a framework. */
class AppContainer(context: Context) {
    val appScope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    val preferences = AppPreferences(context)
    val unlocks = UnlockBus()
    val cookieJar = PersistentCookieJar(
        SharedPrefsCookieStore(context.getSharedPreferences("backhog_cookies", Context.MODE_PRIVATE)),
    )
    val session = SessionManager(preferences, cookieJar, appScope)
}
