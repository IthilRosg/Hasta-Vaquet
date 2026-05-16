package com.hastavaquet

import android.content.Intent
import android.content.pm.PackageManager
import android.net.VpnService
import android.os.Bundle
import android.widget.Toast
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.result.contract.ActivityResultContracts
import androidx.core.content.ContextCompat
import androidx.compose.runtime.*
import com.hastavaquet.ui.MainScreen
import com.hastavaquet.HastaVaquetTheme
import org.json.JSONObject

class MainActivity : ComponentActivity() {

    private var configState = mutableStateOf<String?>(null)
    private val profilesState = mutableStateOf<List<ProfileInfo>>(emptyList())
    private val selectedProfileState = mutableStateOf<String?>(null)

    data class ProfileInfo(
        val name: String,
        val serverIp: String,
        val port: Int,
        val configJson: String
    )

    // ─── Лаунчеры ─────────────────────────────────────────────────────────

    private val filePickerLauncher = registerForActivityResult(
        ActivityResultContracts.OpenDocument()
    ) { uri ->
        uri?.let { readConfigFromFile(it) }
    }

    private val cameraPermissionLauncher = registerForActivityResult(
        ActivityResultContracts.RequestPermission()
    ) { granted ->
        if (granted) startScanner()
        else Toast.makeText(this, "Разрешение на камеру необходимо", Toast.LENGTH_SHORT).show()
    }

    private val scanResultLauncher = registerForActivityResult(
        ActivityResultContracts.StartActivityForResult()
    ) { result ->
        if (result.resultCode == RESULT_OK) {
            result.data?.getStringExtra(ScannerActivity.EXTRA_CONFIG)?.let {
                addProfile(it)
            }
        }
    }

    private val vpnPermissionLauncher = registerForActivityResult(
        ActivityResultContracts.StartActivityForResult()
    ) { result ->
        if (result.resultCode == RESULT_OK) {
            pendingConfig?.let { config ->
                val intent = Intent(this, HastaVaquetVpnService::class.java)
                intent.putExtra("config", config)
                startForegroundService(intent)
            }
        }
        pendingConfig = null
    }

    // ─── Lifecycle ─────────────────────────────────────────────────────────

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        AppLogger.init(this)
        loadProfiles()

        setContent {
            LaunchedEffect(intent) {
                intent?.getStringExtra("config")?.let { addProfile(it) }
            }

            HastaVaquetTheme {
                MainScreen(
                    profiles = profilesState.value,
                    selectedProfile = selectedProfileState.value,
                    onSelectProfile = { name -> selectProfile(name) },
                    onDeleteProfile = { name -> deleteProfile(name) },
                    onConnect = { config -> startVpn(config) },
                    onDisconnect = { stopVpn() },
                    onScanQR = { checkCameraPermission() },
                    onAddFromFile = {
                        filePickerLauncher.launch(arrayOf("application/json", "text/plain", "*/*"))
                    }
                )
            }
        }
    }

    // ─── Профили ───────────────────────────────────────────────────────────

    private fun loadProfiles() {
        val prefs = getSharedPreferences("profiles", MODE_PRIVATE)
        val names = prefs.getStringSet("names", emptySet()) ?: emptySet()
        val list = names.mapNotNull { name ->
            prefs.getString("cfg_$name", null)?.let { json ->
                val info = parseProfileInfo(name, json)
                if (info != null) info else null
            }
        }.sortedBy { it.name }
        profilesState.value = list
        selectedProfileState.value = prefs.getString("selected", null)
    }

    private fun parseProfileInfo(name: String, json: String): ProfileInfo? {
        return try {
            val obj = JSONObject(json)
            ProfileInfo(
                name = name,
                serverIp = obj.optString("server_ip", "—"),
                port = obj.optInt("port", 9999),
                configJson = json
            )
        } catch (_: Exception) { null }
    }

    private fun addProfile(config: String) {
        var name = ""
        try {
            name = JSONObject(config).optString("profile_name", "")
        } catch (_: Exception) {}
        if (name.isBlank()) name = "Profile-${System.currentTimeMillis() % 10000}"

        val prefs = getSharedPreferences("profiles", MODE_PRIVATE)
        val names = (prefs.getStringSet("names", emptySet()) ?: emptySet()).toMutableSet()
        names.add(name)
        prefs.edit()
            .putStringSet("names", names)
            .putString("cfg_$name", config)
            .apply()

        configState.value = config
        loadProfiles()
        selectProfile(name)
        Toast.makeText(this, "Профиль \"$name\" добавлен", Toast.LENGTH_SHORT).show()
    }

    private fun selectProfile(name: String) {
        selectedProfileState.value = name
        getSharedPreferences("profiles", MODE_PRIVATE)
            .edit().putString("selected", name).apply()
        val profile = profilesState.value.find { it.name == name }
        configState.value = profile?.configJson
    }

    private fun deleteProfile(name: String) {
        val prefs = getSharedPreferences("profiles", MODE_PRIVATE)
        val names = (prefs.getStringSet("names", emptySet()) ?: emptySet()).toMutableSet()
        names.remove(name)
        prefs.edit()
            .putStringSet("names", names)
            .remove("cfg_$name")
            .apply()
        if (selectedProfileState.value == name) {
            selectedProfileState.value = null
            configState.value = null
            prefs.edit().remove("selected").apply()
        }
        loadProfiles()
    }

    // ─── VPN ───────────────────────────────────────────────────────────────

    private var pendingConfig: String? = null

    private fun startVpn(config: String) {
        val intent = Intent(this, HastaVaquetVpnService::class.java)
        intent.putExtra("config", config)

        val vpnIntent = VpnService.prepare(this)
        if (vpnIntent != null) {
            pendingConfig = config
            vpnPermissionLauncher.launch(vpnIntent)
        } else {
            startForegroundService(intent)
        }
    }

    private fun stopVpn() {
        // Форсированная остановка: Go core + Android service
        core.Core.stopVPN()
        stopService(Intent(this, HastaVaquetVpnService::class.java))
    }

    // ─── Файлы и сканер ────────────────────────────────────────────────────

    private fun readConfigFromFile(uri: android.net.Uri) {
        try {
            contentResolver.openInputStream(uri)?.use { inputStream ->
                addProfile(inputStream.bufferedReader().readText())
            }
        } catch (e: Exception) {
            Toast.makeText(this, "Ошибка чтения файла: ${e.message}", Toast.LENGTH_LONG).show()
        }
    }

    private fun checkCameraPermission() {
        if (ContextCompat.checkSelfPermission(this, android.Manifest.permission.CAMERA) == PackageManager.PERMISSION_GRANTED) {
            startScanner()
        } else {
            cameraPermissionLauncher.launch(android.Manifest.permission.CAMERA)
        }
    }

    private fun startScanner() {
        scanResultLauncher.launch(Intent(this, ScannerActivity::class.java))
    }
}
