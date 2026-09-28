package com.collinpendleton.backhog

import android.app.Application
import coil3.ImageLoader
import coil3.PlatformContext
import coil3.SingletonImageLoader
import coil3.network.okhttp.OkHttpNetworkFetcherFactory
import coil3.request.crossfade
import okhttp3.OkHttpClient

class BackhogApp : Application(), SingletonImageLoader.Factory {
    lateinit var container: AppContainer
        private set

    override fun onCreate() {
        super.onCreate()
        container = AppContainer(this)
        container.session.start()
    }

    /**
     * The app-wide image loader: covers via the public cover endpoints, IGDB
     * CDN art for screenshots and related games, and the reader's
     * illustrations — which are cookie-authenticated, same-origin asset
     * requests, exactly like the audio stream. The cookie jar is
     * host-scoped, so a third-party CDN request carries nothing.
     */
    override fun newImageLoader(context: PlatformContext): ImageLoader =
        ImageLoader.Builder(context)
            .components {
                add(
                    OkHttpNetworkFetcherFactory(
                        callFactory = OkHttpClient.Builder()
                            .cookieJar(container.cookieJar)
                            .build(),
                    ),
                )
            }
            .crossfade(true)
            .build()
}
