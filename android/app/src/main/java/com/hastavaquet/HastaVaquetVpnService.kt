package com.hastavaquet

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.net.VpnService
import android.os.ParcelFileDescriptor
import core.Core
import java.io.FileDescriptor

/**
 * Android VpnService — создаёт TUN-интерфейс и передаёт fd в Go-ядро.
 */
class HastaVaquetVpnService : VpnService() {

    private var tunFd: ParcelFileDescriptor? = null
    private var configJson: String = ""

    companion object {
        const val NOTIFICATION_CHANNEL = "hasta-vaquet-vpn"
        const val NOTIFICATION_ID = 1
    }

    override fun onCreate() {
        super.onCreate()
        createNotificationChannel()
        startForeground(NOTIFICATION_ID, buildNotification())
    }

    override fun onStartCommand(intent: android.content.Intent?, flags: Int, startId: Int): Int {
        if (intent?.hasExtra("config") == true) {
            configJson = intent.getStringExtra("config") ?: ""
        }
        val builder = Builder()
        builder.setSession("Hasta-Vaquet")
        builder.setMtu(1300)

        // Разбор конфига для настройки TUN
        val cfg = parseConfig(configJson)
        builder.addAddress(cfg.internalIp, 24)
        builder.addRoute("0.0.0.0", 0)

        // Smart Bypass — исключить РФ-приложения из VPN
        for (pkg in cfg.bypassPackages) {
            builder.addDisallowedApplication(pkg)
        }

        tunFd = builder.establish()
        if (tunFd == null) {
            stopSelf()
            return START_NOT_STICKY
        }

        // Запуск Go-ядра: StartVPN(configJson, fd)
        val fd: Int = tunFd!!.detachFd()
        val result = Core.startVPN(configJson, fd.toLong())
        if (result != "ok") {
            android.util.Log.e("HastaVaquet", "Core.startVPN failed: $result")
            stopSelf()
            return START_NOT_STICKY
        }
        android.util.Log.i("HastaVaquet", "VPN started OK, fd=$fd")
        return START_STICKY
    }

    override fun onRevoke() {
        Core.stopVPN()
        tunFd?.close()
        tunFd = null
        stopForeground(STOP_FOREGROUND_REMOVE)
        stopSelf()
    }

    override fun onDestroy() {
        Core.stopVPN()
        tunFd?.close()
        tunFd = null
        super.onDestroy()
    }

    private fun parseConfig(json: String): VpnConfig {
        // Минимальный парсинг — всё основное делает Go
        val cfg = VpnConfig()
        try {
            val obj = org.json.JSONObject(json)
            cfg.internalIp = obj.optString("internal_ip", "10.0.0.10")
            val bypass = obj.optJSONArray("bypass_packages")
            if (bypass != null) {
                for (i in 0 until bypass.length()) {
                    cfg.bypassPackages.add(bypass.getString(i))
                }
            }
        } catch (_: Exception) {}
        return cfg
    }

    private fun createNotificationChannel() {
        val channel = NotificationChannel(
            NOTIFICATION_CHANNEL, "Hasta-Vaquet VPN",
            NotificationManager.IMPORTANCE_LOW
        )
        val nm = getSystemService(NotificationManager::class.java)
        nm.createNotificationChannel(channel)
    }

    private fun buildNotification(): Notification {
        return Notification.Builder(this, NOTIFICATION_CHANNEL)
            .setContentTitle("Hasta-Vaquet")
            .setContentText("VPN подключён")
            .setSmallIcon(android.R.drawable.ic_lock_lock)
            .setOngoing(true)
            .build()
    }
}

data class VpnConfig(
    var internalIp: String = "10.0.0.10",
    val bypassPackages: MutableList<String> = mutableListOf()
)
