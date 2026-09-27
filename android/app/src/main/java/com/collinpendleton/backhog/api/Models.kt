package com.collinpendleton.backhog.api

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

// Transcribed from web/src/lib/types.ts, which stays the source of truth.
// Grow this file per stage; unknown fields are ignored by the client's Json.

@Serializable
enum class Role {
    @SerialName("admin") Admin,
    @SerialName("member") Member,
    @SerialName("reader") Reader;

    val label: String
        get() = when (this) {
            Admin -> "Administrator"
            Member -> "Member"
            Reader -> "Reader"
        }

    val blurb: String
        get() = when (this) {
            Admin -> "Everything, plus accounts, invites and server settings."
            Member -> "The whole app, including attaching files and scanning the library."
            Reader -> "Reads, listens and tracks their own shelf. Cannot manage library files."
        }
}

@Serializable
data class User(
    val id: String,
    val email: String,
    val username: String,
    val role: Role,
    @SerialName("disabled_at") val disabledAt: String? = null,
    @SerialName("created_at") val createdAt: String,
) {
    /** True for everyone but a reader — mirrors the server's RequireMediaManager. */
    val canManageMedia: Boolean get() = role != Role.Reader
    val isAdmin: Boolean get() = role == Role.Admin
}

/** What the sign-in screens read before anyone has an account. */
@Serializable
data class AuthConfig(
    @SerialName("registration_enabled") val registrationEnabled: Boolean,
    /** No accounts exist yet: the first sign-up is allowed and becomes the administrator. */
    val setup: Boolean = false,
    val invite: InviteOffer? = null,
)

@Serializable
data class InviteOffer(
    val email: String,
    val role: Role,
    @SerialName("invited_by") val invitedBy: String,
    @SerialName("expires_at") val expiresAt: String,
)

@Serializable
data class Health(
    val status: String,
    val metadata: Boolean = false,
    val steam: Boolean = false,
)

@Serializable
data class Ok(val ok: Boolean = true)

@Serializable
data class LoginRequest(val email: String, val password: String)

@Serializable
data class RegisterRequest(
    val email: String,
    val username: String,
    val password: String,
    val invite: String? = null,
)

@Serializable
data class ChangePasswordRequest(
    @SerialName("current_password") val currentPassword: String,
    @SerialName("new_password") val newPassword: String,
)

/** The server's error body: `{"error": "..."}`. */
@Serializable
internal data class ErrorBody(val error: String? = null)
