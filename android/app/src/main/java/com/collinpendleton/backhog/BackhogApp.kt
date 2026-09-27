package com.collinpendleton.backhog

import android.app.Application

class BackhogApp : Application() {
    lateinit var container: AppContainer
        private set

    override fun onCreate() {
        super.onCreate()
        container = AppContainer(this)
        container.session.start()
    }
}
