package com.hastavaquet

import android.content.Intent
import android.content.pm.PackageManager
import android.net.VpnService
import android.os.Bundle
import android.widget.Toast
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.*
import androidx.core.content.ContextCompat
import androidx.compose.runtime.*
import com.hastavaquet.ui.MainScreen
import com.hastavaquet.HastaVaquetTheme

class MainActivity : ComponentActivity() {

    private var configState = mutableStateOf<String?>(null)

    // Лаунчер для выбора JSON-файла
    private val filePickerLauncher = registerForActivityResult(
        ActivityResultContracts.OpenDocument()
    ) { uri ->
        uri?.let {
            readConfigFromFile(it)
        }
    }

    private val requestPermissionLauncher = registerForActivityResult(
        ActivityResultContracts.RequestPermission()
    ) { isGranted: Boolean ->
        if (isGranted) {
            startScanner()
        } else {
            Toast.makeText(this, "Разрешение на камеру необходимо для сканирования QR", Toast.LENGTH_SHORT).show()
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        setContent {
            // Инициализируем стейт из интента один раз
            LaunchedEffect(intent) {
                intent?.getStringExtra("config")?.let {
                    configState.value = it
                }
            }

            HastaVaquetTheme {
                MainScreen(
                    initialConfig = configState.value,
                    onConnect = { config ->
                        startVpn(config)
                    },
                    onDisconnect = {
                        stopVpn()
                    },
                    onScanQR = {
                        checkCameraPermission()
                    },
                    onAddFromFile = {
                        filePickerLauncher.launch(arrayOf("application/json", "text/plain", "*/*"))
                    }
                )
            }
        }
    }

    private fun startVpn(config: String) {
        val intent = Intent(this, HastaVaquetVpnService::class.java)
        intent.putExtra("config", config)

        // Проверяем права на VPN
        val vpnIntent = VpnService.prepare(this)
        if (vpnIntent != null) {
            pendingConfig = config
            startActivityForResult(vpnIntent, VPN_REQUEST_CODE)
        } else {
            startForegroundService(intent)
        }
    }

    private fun readConfigFromFile(uri: android.net.Uri) {
        try {
            contentResolver.openInputStream(uri)?.use { inputStream ->
                val reader = inputStream.bufferedReader()
                val config = reader.readText()
                configState.value = config
                Toast.makeText(this, "Конфигурация загружена", Toast.LENGTH_SHORT).show()
            }
        } catch (e: Exception) {
            Toast.makeText(this, "Ошибка чтения файла: ${e.message}", Toast.LENGTH_LONG).show()
        }
    }

    private fun stopVpn() {
        val intent = Intent(this, HastaVaquetVpnService::class.java)
        stopService(intent)
    }

    private fun checkCameraPermission() {
        when {
            ContextCompat.checkSelfPermission(this, android.Manifest.permission.CAMERA) == PackageManager.PERMISSION_GRANTED -> {
                startScanner()
            }
            else -> {
                requestPermissionLauncher.launch(android.Manifest.permission.CAMERA)
            }
        }
    }

    private val scanResultLauncher = registerForActivityResult(
        ActivityResultContracts.StartActivityForResult()
    ) { result ->
        if (result.resultCode == RESULT_OK) {
            val config = result.data?.getStringExtra(ScannerActivity.EXTRA_CONFIG)
            if (config != null) {
                configState.value = config
                Toast.makeText(this, "QR-код распознан", Toast.LENGTH_SHORT).show()
            }
        }
    }

    private fun startScanner() {
        val intent = Intent(this, ScannerActivity::class.java)
        scanResultLauncher.launch(intent)
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
