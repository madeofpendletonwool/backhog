package com.collinpendleton.backhog

import android.app.Application
import com.collinpendleton.backhog.api.ApiClient
import com.collinpendleton.backhog.api.PersistentCookieJar
import com.collinpendleton.backhog.auth.SessionManager
import com.collinpendleton.backhog.data.SettingsStore
import java.io.File

/**
 * The app's tiny object graph — one settings store, one cookie jar, one HTTP
 * stack, one session. No DI framework: the app is a single-activity client
 * for one server, and four objects do not need one.
 */
class BackhogApplication : Application() {

    lateinit var settings: SettingsStore
        private set
    lateinit var api: ApiClient
        private set
    lateinit var session: SessionManager
        private set

    override fun onCreate() {
        super.onCreate()
        settings = SettingsStore(this)
        val cookieJar = PersistentCookieJar(File(filesDir, "cookies.json"))
        api = ApiClient(this, cookieJar)
        session = SessionManager(settings, api)
    }
}
