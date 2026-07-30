package com.clipbridge.app.presentation.theme

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.compositeOver

val BridgeBlue = Color(0xFF3F5F90)
val BridgeIndigo = Color(0xFF62558C)
val BridgeGreen = Color(0xFF44765B)
val BridgeOrange = Color(0xFF8C5D3C)

private fun lightColors(primary: Color) = lightColorScheme(
    primary = primary,
    onPrimary = Color.White,
    primaryContainer = primary.copy(alpha = 0.14f).compositeOver(Color(0xFFF8F9FC)),
    onPrimaryContainer = Color(0xFF20242B),
    secondary = primary,
    onSecondary = Color.White,
    secondaryContainer = Color(0xFFE9EBF0),
    onSecondaryContainer = Color(0xFF25272C),
    tertiary = BridgeGreen,
    onTertiary = Color.White,
    tertiaryContainer = Color(0xFFE1ECE5),
    onTertiaryContainer = Color(0xFF24372B),
    background = Color(0xFFF7F8FA),
    onBackground = Color(0xFF1D1F23),
    surface = Color(0xFFFFFFFF),
    onSurface = Color(0xFF1D1F23),
    surfaceVariant = Color(0xFFF0F1F4),
    onSurfaceVariant = Color(0xFF5B5E66),
    outline = Color(0xFF777A82),
    outlineVariant = Color(0xFFD9DBE0),
    error = Color(0xFFBA1A1A),
)

private fun darkColors(primary: Color) = darkColorScheme(
    primary = primary.copy(alpha = 0.75f).compositeOver(Color.White),
    onPrimary = Color(0xFF17202F),
    primaryContainer = primary.copy(alpha = 0.45f).compositeOver(Color(0xFF1B1D21)),
    onPrimaryContainer = Color(0xFFE7E9EE),
    secondary = primary.copy(alpha = 0.75f).compositeOver(Color.White),
    onSecondary = Color(0xFF202226),
    secondaryContainer = Color(0xFF34363B),
    onSecondaryContainer = Color(0xFFE5E6E9),
    tertiary = Color(0xFF72DCB6),
    onTertiary = Color(0xFF003828),
    tertiaryContainer = Color(0xFF00513C),
    onTertiaryContainer = Color(0xFFC2F3DF),
    background = Color(0xFF11131A),
    onBackground = Color(0xFFE3E1EA),
    surface = Color(0xFF191B23),
    onSurface = Color(0xFFE3E1EA),
    surfaceVariant = Color(0xFF45464F),
    onSurfaceVariant = Color(0xFFC7C6D0),
)

private fun themePrimary(themeColor: String): Color = when (themeColor) {
    "green" -> BridgeGreen
    "purple" -> BridgeIndigo
    "orange" -> BridgeOrange
    "neutral" -> Color(0xFF555B66)
    else -> BridgeBlue
}

@Composable
fun ClipBridgeTheme(
    themeColor: String = "purple",
    content: @Composable () -> Unit,
) {
    val primary = themePrimary(themeColor)
    MaterialTheme(
        colorScheme = if (isSystemInDarkTheme()) darkColors(primary) else lightColors(primary),
        content = content,
    )
}
