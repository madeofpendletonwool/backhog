package com.collinpendleton.backhog.ui.auth

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.collinpendleton.backhog.api.ApiClient
import com.collinpendleton.backhog.auth.SessionManager
import com.collinpendleton.backhog.theme.LocalBackhogPalette
import com.collinpendleton.backhog.ui.components.BackhogField
import com.collinpendleton.backhog.ui.components.Banner
import com.collinpendleton.backhog.ui.components.BannerKind
import com.collinpendleton.backhog.ui.components.BodyText
import com.collinpendleton.backhog.ui.components.PrimaryButton

/**
 * "Welcome back" — mirrors the web LoginPage. An invite-only server does not
 * advertise a sign-up form that will only ever answer 403; a fresh install
 * with no accounts still offers one, because that first sign-up is how the
 * server gets an administrator at all.
 */
@Composable
fun LoginScreen(session: SessionManager, api: ApiClient, onRegister: () -> Unit) {
    val p = LocalBackhogPalette.current
    val invite by session.pendingInvite.collectAsState()

    val configState = rememberAuthConfig(api, session, invite)
    var email by remember { mutableStateOf("") }
    var password by remember { mutableStateOf("") }
    var error by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }

    val config = (configState as? AuthConfigState.Ready)?.config
    val canSignUp = config?.registrationEnabled == true || config?.setup == true

    Column(
        Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .imePadding()
            .padding(horizontal = 24.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Spacer(Modifier.height(72.dp))
        Text("🐗", fontSize = 40.sp)
        Spacer(Modifier.height(8.dp))
        Text(
            "Welcome back",
            color = p.c100,
            fontSize = 24.sp,
            fontWeight = FontWeight.Bold,
            fontFamily = p.displayFont,
        )
        Spacer(Modifier.height(4.dp))
        BodyText("Sign in to get back to your backlog.")
        Spacer(Modifier.height(24.dp))

        androidx.compose.foundation.layout.Box(Modifier.fillMaxWidth()) {
            Column(Modifier.fillMaxWidth()) {
                if (invite != null) {
                    Banner(
                        BannerKind.INFO,
                        "You arrived with a sign-up link — create your account with it.",
                    )
                    Spacer(Modifier.height(12.dp))
                }
                if (configState is AuthConfigState.Failed) {
                    Banner(BannerKind.ERROR, configState.message)
                    Spacer(Modifier.height(12.dp))
                }

                BackhogField(
                    value = email,
                    onValueChange = { email = it },
                    label = "Email",
                    placeholder = "you@example.com",
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Email),
                )
                Spacer(Modifier.height(12.dp))
                BackhogField(
                    value = password,
                    onValueChange = { password = it },
                    label = "Password",
                    placeholder = "••••••••",
                    password = true,
                )
                Spacer(Modifier.height(16.dp))

                if (error != null) {
                    Banner(BannerKind.ERROR, error!!)
                    Spacer(Modifier.height(12.dp))
                }

                PrimaryButton(
                    text = "Sign in",
                    onClick = {
                        busy = true
                        error = null
                        session.login(email.trim(), password) { result ->
                            busy = false
                            result.onFailure { e -> error = e.message ?: "Sign-in failed." }
                        }
                    },
                    loading = busy,
                    modifier = Modifier.fillMaxWidth(),
                )
                Spacer(Modifier.height(18.dp))

                when {
                    canSignUp || invite != null -> {
                        Text(
                            if (config?.setup == true) "Set up this server" else "Create an account",
                            color = p.hlBright,
                            fontSize = 14.sp,
                            fontWeight = FontWeight.Medium,
                            modifier =
                                Modifier.clickable(onClick = onRegister).padding(8.dp),
                        )
                    }
                    else -> BodyText("Accounts here are made by invitation.")
                }

                Spacer(Modifier.height(10.dp))
                Text(
                    "Wrong server? Switch.",
                    color = p.c500,
                    fontSize = 13.sp,
                    modifier = Modifier.clickable { session.beginSwitchServer() }.padding(8.dp),
                )
            }
        }
        Spacer(Modifier.height(32.dp))
    }
}
