package com.collinpendleton.backhog

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import com.collinpendleton.backhog.ui.BackhogApp

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        val app = application as BackhogApplication
        setContent { BackhogApp(app.session, app.settings, app.api) }
    }
}
