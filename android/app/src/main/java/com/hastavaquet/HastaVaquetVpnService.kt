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
        android.widget.Toast.makeText(this, "VPN v7 REFLECTION", android.widget.Toast.LENGTH_LONG).show()
        AppLogger.log("VPN", "onStartCommand v7 called")

        // Если сервис запущен для остановки
        if (intent?.getBooleanExtra("stop", false) == true) {
            AppLogger.log("VPN", "stop intent received")
            doStop()
            return START_NOT_STICKY
        }

        if (intent?.hasExtra("config") == true) {
            configJson = intent.getStringExtra("config") ?: ""
            AppLogger.log("VPN", "config received, length=${configJson.length}")
        } else {
            AppLogger.log("VPN", "WARNING: no config in intent!")
        }

        val root = org.json.JSONObject(configJson)
        val srvIp = root.optString("server_ip", "31.42.120.154")
        val srvPort = root.optInt("port", 9999)

        val builder = Builder()
        builder.setSession("Hasta-Vaquet")
        builder.setMtu(1300)

        val cfg = parseConfig(configJson)
        builder.addAddress(cfg.internalIp, 24)
        builder.addRoute("0.0.0.0", 0)

        for (pkg in cfg.bypassPackages) {
            builder.addDisallowedApplication(pkg)
        }

        AppLogger.log("VPN", "establishing TUN...")
        tunFd = builder.establish()
        if (tunFd == null) {
            AppLogger.log("VPN", "ERROR: TUN establish returned null!")
            stopSelf()
            return START_NOT_STICKY
        }

        val fd: Int = tunFd!!.detachFd()
        AppLogger.log("VPN", "TUN fd=$fd, creating UDP in background...")

        // Сеть на фоне (NetworkOnMainThreadException запрещает connect на main thread)
        Thread {
            var udpFd = -1
            try {
                val udpSocket = java.net.DatagramSocket()
                udpSocket.connect(java.net.InetAddress.getByName(srvIp), srvPort)
                protect(udpSocket)
                val implField = java.net.DatagramSocket::class.java.getDeclaredField("impl")
                implField.isAccessible = true
                val impl = implField.get(udpSocket)
                val fdField = impl.javaClass.getDeclaredField("fd")
                fdField.isAccessible = true
                val fileDesc = fdField.get(impl) as java.io.FileDescriptor
                val pfd = android.os.ParcelFileDescriptor.dup(fileDesc)
                udpFd = pfd.detachFd()
                udpSocket.close()
                AppLogger.log("VPN", "UDP fd=$udpFd protected, target=$srvIp:$srvPort")
                val result = Core.startVPN(configJson, fd.toLong(), udpFd.toLong())
                AppLogger.log("VPN", "Core.startVPN result: $result")
                if (result != "ok") doStop()
            } catch (e: Exception) {
                AppLogger.log("VPN", "ERROR: ${e.javaClass.simpleName}: ${e.message}")
                doStop()
            }
        }.start()

        return START_STICKY
    }

    private fun doStop() {
        AppLogger.log("VPN", "doStop called")
        Core.stopVPN()
        tunFd?.close()
        tunFd = null
        stopForeground(STOP_FOREGROUND_REMOVE)
        stopSelf()
    }

    override fun onRevoke() {
        AppLogger.log("VPN", "onRevoke called")
        Core.stopVPN()
        tunFd?.close()
        tunFd = null
        stopForeground(STOP_FOREGROUND_REMOVE)
        stopSelf()
    }

    override fun onDestroy() {
        AppLogger.log("VPN", "onDestroy called")
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
