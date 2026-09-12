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
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
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
import com.collinpendleton.backhog.api.ROLE_COPY
import com.collinpendleton.backhog.theme.LocalBackhogPalette
import com.collinpendleton.backhog.ui.components.BackhogField
import com.collinpendleton.backhog.ui.components.BackhogPanel
import com.collinpendleton.backhog.ui.components.Banner
import com.collinpendleton.backhog.ui.components.BannerKind
import com.collinpendleton.backhog.ui.components.BodyText
import com.collinpendleton.backhog.ui.components.PrimaryButton
import com.collinpendleton.backhog.ui.components.Spinner

/**
 * "Start hogging" — mirrors the web RegisterPage, invite semantics included:
 * the token resolves into the offer it represents (email + role), an unknown
 * or spent one shows the stale-link notice, and a server with the door shut
 * and no key in hand says so plainly rather than showing a form whose only
 * possible outcome is a 403.
 */
@Composable
fun RegisterScreen(session: SessionManager, api: ApiClient, onLogin: () -> Unit) {
    val p = LocalBackhogPalette.current
    val invite by session.pendingInvite.collectAsState()

    val configState = rememberAuthConfig(api, session, invite)
    var email by remember { mutableStateOf("") }
    var username by remember { mutableStateOf("") }
    var password by remember { mutableStateOf("") }
    var error by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }

    val config = (configState as? AuthConfigState.Ready)?.config
    val invited = config?.invite != null

    // The invite's email pre-fills the form once, and stays editable: it is
    // who the link was cut for, not a constraint the server enforces.
    var prefilled by remember { mutableStateOf(false) }
    LaunchedEffect(config?.invite?.email) {
        if (!prefilled && config?.invite?.email != null) {
            prefilled = true
            email = config.invite!!.email
        }
    }

    Column(
        Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .imePadding()
            .padding(horizontal = 24.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Spacer(Modifier.height(56.dp))
        Text("🐗", fontSize = 36.sp)
        Spacer(Modifier.height(8.dp))
        Text(
            "Start hogging",
            color = p.c100,
            fontSize = 24.sp,
            fontWeight = FontWeight.Bold,
            fontFamily = p.displayFont,
        )
        Spacer(Modifier.height(4.dp))

        val subtitle =
            when {
                config?.setup == true -> "Nobody has an account here yet, so this one runs the place."
                invited -> "${config?.invite?.invitedBy ?: "Someone"} invited you."
                else -> "Track the games you own, and the ones you'll get to eventually."
            }
        BodyText(subtitle)
        Spacer(Modifier.height(24.dp))

        when (configState) {
            is AuthConfigState.Loading -> {
                Spacer(Modifier.height(24.dp))
                Spinner()
            }
            is AuthConfigState.Failed -> {
                Banner(BannerKind.ERROR, configState.message)
            }
            is AuthConfigState.Ready -> {
                val cfg = configState.config
                // The door is shut and this caller has no key.
                val open = cfg.registrationEnabled || cfg.setup
                if (!invited && !open) {
                    BackhogPanel(Modifier.fillMaxWidth()) {
                        Text(
                            "Invite only",
                            color = p.c100,
                            fontSize = 17.sp,
                            fontWeight = FontWeight.SemiBold,
                            fontFamily = p.displayFont,
                        )
                        Spacer(Modifier.height(4.dp))
                        BodyText(
                            "This Backhog is not taking sign-ups. Accounts here are made by " +
                                "invitation — ask whoever runs this server for a sign-up link.",
                        )
                    }
                } else {
                    // A token that came back unresolved is spent, expired or
                    // was never real; the server does not distinguish, and
                    // neither does this.
                    val staleToken = invite != null && !invited
                    if (staleToken) {
                        Banner(
                            BannerKind.ERROR,
                            "That sign-up link is no longer valid — it may have been used " +
                                "already, withdrawn, or simply run out. Ask for a fresh one.",
                        )
                        Spacer(Modifier.height(12.dp))
                    }

                    if (cfg.invite != null) {
                        Surface(
                            shape = RoundedCornerShape(p.radiusField),
                            color = p.c850,
                            modifier = Modifier.fillMaxWidth(),
                        ) {
                            Column(Modifier.padding(12.dp)) {
                                Text(
                                    "You are joining as a ${ROLE_COPY.getValue(cfg.invite.role).label.lowercase()}.",
                                    color = p.c200,
                                    fontSize = 14.sp,
                                    fontWeight = FontWeight.Medium,
                                )
                                Spacer(Modifier.height(2.dp))
                                Text(
                                    ROLE_COPY.getValue(cfg.invite.role).blurb,
                                    color = p.c500,
                                    fontSize = 12.sp,
                                    lineHeight = 16.sp,
                                )
                            }
                        }
                        Spacer(Modifier.height(12.dp))
                    }

                    if (cfg.setup) {
                        Banner(
                            BannerKind.INFO,
                            "The first account on a server becomes its administrator: it manages " +
                                "the other accounts, the invites and the settings.",
                        )
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
                        value = username,
                        onValueChange = { username = it },
                        label = "Username",
                        placeholder = "backlogslayer",
                    )
                    Spacer(Modifier.height(12.dp))
                    BackhogField(
                        value = password,
                        onValueChange = { password = it },
                        label = "Password",
                        placeholder = "At least 8 characters",
                        password = true,
                    )
                    Spacer(Modifier.height(16.dp))

                    if (error != null) {
                        Banner(BannerKind.ERROR, error!!)
                        Spacer(Modifier.height(12.dp))
                    }

                    PrimaryButton(
                        text = "Create account",
                        onClick = {
                            if (password.length < 8) {
                                error = "Password must be at least 8 characters."
                                return@PrimaryButton
                            }
                            busy = true
                            error = null
                            session.register(email.trim(), username.trim(), password, invite) { result ->
                                busy = false
                                result.onFailure { e -> error = e.message ?: "Could not create the account." }
                            }
                        },
                        loading = busy,
                        modifier = Modifier.fillMaxWidth(),
                    )
                }
            }
        }

        Spacer(Modifier.height(18.dp))
        Text(
            "Already have an account? Sign in",
            color = p.hlBright,
            fontSize = 14.sp,
            fontWeight = FontWeight.Medium,
            modifier = Modifier.clickable(onClick = onLogin).padding(8.dp),
        )
        Spacer(Modifier.height(32.dp))
    }
}
