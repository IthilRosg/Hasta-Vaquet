package com.hastavaquet.ui

import androidx.compose.animation.core.*
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

@Composable
fun MainScreen(
    initialConfig: String?,
    onConnect: (String) -> Unit,
    onDisconnect: () -> Unit,
    onScanQR: () -> Unit,
    onAddFromFile: () -> Unit
) {
    var connected by remember { mutableStateOf(false) }
    var statusText by remember { mutableStateOf("Готов к подключению") }
    var configJson by remember { mutableStateOf(initialConfig ?: "") }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(MaterialTheme.colorScheme.background)
            .padding(24.dp),
        horizontalAlignment = Alignment.CenterHorizontally
    ) {
        Spacer(Modifier.height(60.dp))

        // Логотип
        Text(
            "Hasta-Vaquet",
            style = MaterialTheme.typography.headlineLarge,
            fontWeight = FontWeight.Bold,
            color = MaterialTheme.colorScheme.onBackground
        )
        Spacer(Modifier.height(8.dp))
        Text(
            "Безопасное VPN-подключение",
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant
        )

        Spacer(Modifier.height(48.dp))

        // Кнопка Connect (круглая, как в Windows GUI)
        val infiniteTransition = rememberInfiniteTransition()
        val pulseAlpha by infiniteTransition.animateFloat(
            initialValue = 0.3f, targetValue = 0.0f,
            animationSpec = infiniteRepeatable(
                tween(2000), RepeatMode.Reverse
            )
        )

        Box(
            modifier = Modifier
                .size(140.dp)
                .clip(CircleShape)
                .background(
                    if (connected) Brush.radialGradient(
                        listOf(
                            Color(0x333FB950),
                            Color(0x003FB950)
                        )
                    ) else Brush.radialGradient(listOf(
                        Color(0x3358A6FF),
                        Color(0x0058A6FF)
                    ))
                )
                .border(
                    width = 2.dp,
                    color = if (connected) Color(0xFF3FB950)
                    else MaterialTheme.colorScheme.outline,
                    shape = CircleShape
                )
                .clickable {
                    if (connected) {
                        onDisconnect()
                        connected = false
                        statusText = "Отключено"
                    } else if (configJson.isNotEmpty()) {
                        onConnect(configJson)
                        connected = true
                        statusText = "Подключаюсь..."
                    }
                },
            contentAlignment = Alignment.Center
        ) {
            Column(horizontalAlignment = Alignment.CenterHorizontally) {
                Text(
                    if (connected) "⬆" else "⬇",
                    fontSize = 36.sp,
                    color = if (connected) Color(0xFF3FB950)
                    else MaterialTheme.colorScheme.onSurface
                )
                Text(
                    if (connected) "Отключить" else "Подключить",
                    style = MaterialTheme.typography.labelSmall,
                    color = if (connected) Color(0xFF3FB950)
                    else MaterialTheme.colorScheme.onSurfaceVariant
                )
            }
        }

        Spacer(Modifier.height(12.dp))
        Text(
            statusText,
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant
        )

        Spacer(Modifier.height(32.dp))

        // Карточки статистики (active only когда connected)
        if (connected) {
            StatsGrid()
            Spacer(Modifier.height(16.dp))
        }

        // Кнопка сканирования QR
        Button(
            onClick = { onScanQR() },
            modifier = Modifier.fillMaxWidth(),
            colors = ButtonDefaults.buttonColors(
                containerColor = MaterialTheme.colorScheme.surface
            ),
            shape = RoundedCornerShape(10.dp)
        ) {
            Text("📷 Сканировать QR-код")
        }

        Spacer(Modifier.height(8.dp))

        // Кнопка добавления файла
        OutlinedButton(
            onClick = { onAddFromFile() },
            modifier = Modifier.fillMaxWidth(),
            shape = RoundedCornerShape(10.dp)
        ) {
            Text("Добавить из файла")
        }

        Spacer(Modifier.height(16.dp))

        // Версия
        Text(
            "v0.0.1",
            style = MaterialTheme.typography.labelSmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant
        )
    }
}

@Composable
private fun StatsGrid() {
    val stats = remember { mutableStateOf(StatsData()) }

    // TODO: Phase 8 — Poll Core.GetStats() каждую секунду
    // LaunchedEffect(Unit) {
    //     while (true) {
    //         val json = Core.getStats()
    //         // парсинг json -> stats.value
    //         delay(1000)
    //     }
    // }

    Column(modifier = Modifier.fillMaxWidth()) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(12.dp)
        ) {
            StatCard("SPEED", "↑ ${stats.value.txSpeed}\n↓ ${stats.value.rxSpeed}", Modifier.weight(1f))
            StatCard("DATA", "↑ ${stats.value.totalTx}\n↓ ${stats.value.totalRx}", Modifier.weight(1f))
        }
        Spacer(Modifier.height(12.dp))
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(12.dp)
        ) {
            StatCard("NETWORK", "Ping: ${stats.value.ping}ms\nLoss: ${stats.value.loss}%", Modifier.weight(1f))
            StatCard("UPTIME", "00:00", Modifier.weight(1f))
        }
    }
}

@Composable
private fun StatCard(title: String, body: String, modifier: Modifier = Modifier) {
    Card(
        modifier = modifier,
        colors = CardDefaults.cardColors(
            containerColor = MaterialTheme.colorScheme.surface
        ),
        shape = RoundedCornerShape(12.dp)
    ) {
        Column(modifier = Modifier.padding(16.dp)) {
            Text(
                title,
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                letterSpacing = 1.sp
            )
            Spacer(Modifier.height(8.dp))
            Text(
                body,
                style = MaterialTheme.typography.bodySmall,
                fontWeight = FontWeight.SemiBold,
                color = MaterialTheme.colorScheme.onSurface
            )
        }
    }
}

data class StatsData(
    val txSpeed: String = "0 B/s",
    val rxSpeed: String = "0 B/s",
    val totalTx: String = "0 B",
    val totalRx: String = "0 B",
    val ping: Int = 0,
    val loss: Int = 0,
    val uptime: String = "00:00"
)
