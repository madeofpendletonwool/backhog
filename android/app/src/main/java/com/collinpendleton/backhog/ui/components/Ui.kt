package com.collinpendleton.backhog.ui.components

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.collinpendleton.backhog.api.Status
import com.collinpendleton.backhog.theme.LocalBackhogPalette
import com.collinpendleton.backhog.theme.statusInk

/**
 * The component vocabulary the web's CSS families define — .panel, .f-field,
 * .f-btn-gold, .f-btn-soft, .f-btn-danger — as Compose. Every piece reads the
 * live palette, so the family recipe (rounded plastic versus print-flat)
 * follows the theme the reader picked.
 */

@Composable
fun BackhogPanel(
    modifier: Modifier = Modifier,
    content: @Composable ColumnScope.() -> Unit,
) {
    val p = LocalBackhogPalette.current
    Surface(
        modifier = modifier,
        shape = RoundedCornerShape(p.radiusPanel),
        color = p.c900,
        contentColor = p.c100,
        border = BorderStroke(1.dp, p.edge),
        // Light grounds cast real shadows; glows only read on black.
        shadowElevation = if (p.isLight) 2.dp else 0.dp,
    ) {
        Column(Modifier.padding(20.dp), content = content)
    }
}

@Composable
fun SectionTitle(text: String) {
    val p = LocalBackhogPalette.current
    Text(
        text,
        color = p.c200,
        fontSize = 14.sp,
        fontWeight = FontWeight.SemiBold,
        fontFamily = p.displayFont,
    )
}

@Composable
fun BodyText(text: String, modifier: Modifier = Modifier) {
    val p = LocalBackhogPalette.current
    Text(text, color = p.c400, fontSize = 13.sp, lineHeight = 18.sp, modifier = modifier)
}

@Composable
fun FieldLabel(text: String) {
    val p = LocalBackhogPalette.current
    Text(
        text,
        color = p.c300,
        fontSize = 12.sp,
        fontWeight = FontWeight.Medium,
        modifier = Modifier.padding(bottom = 6.dp),
    )
}

/** A text field wearing the family's recipe: c-850 fill, hairline border,
 *  the highlight ramp on focus. */
@Composable
fun BackhogField(
    value: String,
    onValueChange: (String) -> Unit,
    label: String,
    modifier: Modifier = Modifier,
    placeholder: String = "",
    password: Boolean = false,
    keyboardOptions: KeyboardOptions = KeyboardOptions.Default,
) {
    val p = LocalBackhogPalette.current
    Column(modifier) {
        FieldLabel(label)
        OutlinedTextField(
            value = value,
            onValueChange = onValueChange,
            modifier = Modifier.fillMaxWidth(),
            placeholder = { Text(placeholder, color = p.c500) },
            singleLine = true,
            visualTransformation =
                if (password) PasswordVisualTransformation() else VisualTransformation.None,
            keyboardOptions = keyboardOptions,
            shape = RoundedCornerShape(p.radiusField),
            textStyle = androidx.compose.ui.text.TextStyle(color = p.c100, fontSize = 15.sp),
            colors =
                androidx.compose.material3.OutlinedTextFieldDefaults.colors(
                    focusedContainerColor = p.c850,
                    unfocusedContainerColor = p.c850,
                    focusedBorderColor = p.hlBright.copy(alpha = if (p.isLight) 1f else 0.5f),
                    unfocusedBorderColor = p.edgeStrong,
                    focusedTextColor = p.c100,
                    unfocusedTextColor = p.c100,
                    cursorColor = p.hlBright,
                    selectionColors =
                        androidx.compose.foundation.text.selection.TextSelectionColors(
                            backgroundColor = p.hlMid.copy(alpha = 0.5f),
                            handleColor = p.hlMid,
                        ),
                ),
        )
    }
}

/** The primary action — brand violet on white ink in Midnight, a lit ember
 *  with dark ink in the library. */
@Composable
fun PrimaryButton(
    text: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    loading: Boolean = false,
) {
    val p = LocalBackhogPalette.current
    Button(
        onClick = onClick,
        enabled = enabled && !loading,
        modifier = modifier,
        shape = RoundedCornerShape(p.radiusField),
        colors =
            ButtonDefaults.buttonColors(
                containerColor = p.primaryBg,
                contentColor = p.primaryInk,
                disabledContainerColor = p.c700,
                disabledContentColor = p.c500,
            ),
    ) {
        if (loading) {
            CircularProgressIndicator(
                modifier = Modifier.size(16.dp),
                strokeWidth = 2.dp,
                color = p.primaryInk,
            )
            androidx.compose.foundation.layout.Spacer(Modifier.size(8.dp))
        }
        Text(text, fontWeight = FontWeight.SemiBold)
    }
}

@Composable
fun SoftButton(
    text: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
) {
    val p = LocalBackhogPalette.current
    OutlinedButton(
        onClick = onClick,
        enabled = enabled,
        modifier = modifier,
        shape = RoundedCornerShape(p.radiusField),
        border = BorderStroke(1.dp, p.edgeStrong),
        colors =
            ButtonDefaults.outlinedButtonColors(
                containerColor = p.c800,
                contentColor = p.c100,
            ),
    ) {
        Text(text)
    }
}

@Composable
fun DangerButton(text: String, onClick: () -> Unit, modifier: Modifier = Modifier) {
    val p = LocalBackhogPalette.current
    Button(
        onClick = onClick,
        modifier = modifier,
        shape = RoundedCornerShape(p.radiusField),
        colors =
            ButtonDefaults.buttonColors(
                containerColor = p.dangerBg,
                contentColor = p.dangerInk,
            ),
    ) {
        Text(text, fontWeight = FontWeight.SemiBold)
    }
}

enum class BannerKind { OK, ERROR, INFO }

/** A status plate: green for confirmations, red for failures, plain for
 *  context — the same semantic colours the badges use. */
@Composable
fun Banner(kind: BannerKind, text: String, modifier: Modifier = Modifier) {
    val p = LocalBackhogPalette.current
    val ink =
        when (kind) {
            BannerKind.OK -> statusInk(Status.PLAYED, p)
            BannerKind.ERROR -> statusInk(Status.DROPPED, p)
            BannerKind.INFO -> p.c300
        }
    val plateColor =
        when (kind) {
            BannerKind.OK -> Color(0xFF10B981).copy(alpha = if (p.isLight) 0.10f else 0.12f)
            BannerKind.ERROR -> Color(0xFFEF4444).copy(alpha = if (p.isLight) 0.10f else 0.12f)
            BannerKind.INFO -> p.c850
        }
    Surface(
        modifier = modifier.fillMaxWidth(),
        shape = RoundedCornerShape(p.radiusField),
        color = plateColor,
        contentColor = ink,
    ) {
        Text(
            text,
            modifier = Modifier.padding(horizontal = 12.dp, vertical = 10.dp),
            fontSize = 13.sp,
            lineHeight = 18.sp,
        )
    }
}

@Composable
fun Spinner(modifier: Modifier = Modifier) {
    val p = LocalBackhogPalette.current
    CircularProgressIndicator(modifier = modifier.size(24.dp), strokeWidth = 2.5.dp, color = p.c400)
}

@Composable
fun KeyValueRow(key: String, value: String) {
    val p = LocalBackhogPalette.current
    Row(
        Modifier.fillMaxWidth().padding(vertical = 4.dp),
        horizontalArrangement = Arrangement.SpaceBetween,
    ) {
        Text(key, color = p.c500, fontSize = 14.sp)
        Text(value, color = p.c200, fontSize = 14.sp)
    }
}

/** "Sep 9, 2026" — how the web's formatDate says a member-since date. */
fun formatDate(iso: String): String =
    runCatching {
        val instant = java.time.Instant.parse(iso)
        val formatter = java.time.format.DateTimeFormatter.ofPattern("MMM d, yyyy")
        formatter.format(instant.atZone(java.time.ZoneId.systemDefault()))
    }.getOrDefault(iso)
