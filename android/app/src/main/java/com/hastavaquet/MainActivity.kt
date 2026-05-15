package com.hastavaquet

import android.content.Intent
import android.net.VpnService
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.layout.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.hastavaquet.ui.ConnectScreen

class MainActivity : ComponentActivity() {

    private var configJson: String? = null

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        // Проверка QR-сканера (если приложение запущено из ссылки или интента)
        configJson = intent?.getStringExtra("config")

        setContent {
            HastaVaquetTheme {
                MainScreen(
                    initialConfig = configJson,
                    onConnect = { config ->
                        startVpn(config)
                    },
                    onDisconnect = {
                        stopVpn()
                    }
                )
            }
        }
    }

    private fun startVpn(config: String) {
        val intent = Intent(this, HastaVaquetVpnService::class.java)
        intent.putExtra("config", config)

        // VpnService требует разрешения пользователя
        val vpnIntent = VpnService.prepare(this)
        if (vpnIntent != null) {
            startActivityForResult(vpnIntent, VPN_REQUEST_CODE)
            pendingConfig = config
        } else {
            startService(intent)
        }
    }

    private fun stopVpn() {
        val intent = Intent(this, HastaVaquetVpnService::class.java)
        stopService(intent)
    }

    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        if (requestCode == VPN_REQUEST_CODE && resultCode == RESULT_OK) {
            pendingConfig?.let { config ->
                val intent = Intent(this, HastaVaquetVpnService::class.java)
                intent.putExtra("config", config)
                startForegroundService(intent)
            }
        }
        pendingConfig = null
    }

    companion object {
        private const val VPN_REQUEST_CODE = 1000
        private var pendingConfig: String? = null
    }
}
