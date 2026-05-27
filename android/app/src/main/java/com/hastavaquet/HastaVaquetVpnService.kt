package com.hastavaquet

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.net.VpnService
import android.os.ParcelFileDescriptor
import core.Core
import core.Protector
import java.io.FileDescriptor

/**
 * Android VpnService — создаёт TUN-интерфейс и передаёт fd в Go-ядро.
 */
class HastaVaquetVpnService : VpnService(), Protector {

    private var tunFd: ParcelFileDescriptor? = null
    private var configJson: String = ""
    private var isRunning: Boolean = false

    override fun protect(fd: Long): Boolean {
        return super.protect(fd.toInt())
    }

    companion object {
        const val NOTIFICATION_CHANNEL = "hasta-vaquet-vpn"
        const val NOTIFICATION_ID = 1

        val SMART_BYPASS_PACKAGES = listOf(
            "ru.sberbankmobile", "ru.sberbank.spasibo",
            "ru.vtb24.mobilebanking.android", "ru.vtb.mobilebank",
            "ru.alfabank.mobile", "ru.alfabank.oavdo",
            "com.tinkoff.core", "com.idamob.tinkoff.android", "ru.tinkoff.mvno",
            "ru.raiffeisen.retail",
            "ru.psbank.mobibank",
            "ru.gazprombank.android",
            "ru.mkb.mobile",
            "ru.openbank.android",
            "ru.rosbank.android",
            "ru.sovcombank.secure",
            "ru.otpbank.android",
            "ru.gosuslugi.android",
            "ru.mos.gosuslugi",
            "ru.nalog.android",
            "ru.mos.mosapp",
        )
    }

    override fun onCreate() {
        super.onCreate()
        createNotificationChannel()
        startForeground(NOTIFICATION_ID, buildNotification())
    }

    override fun onStartCommand(intent: android.content.Intent?, flags: Int, startId: Int): Int {
        val action = intent?.action
        val isStop = intent?.getBooleanExtra("stop", false) == true

        if (isStop || action == "STOP") {
            AppLogger.log("VPN", "Stop requested")
            doStop()
            return START_NOT_STICKY
        }

        // Если это перезапуск после нехватки памяти, а мы уже работали - игнорируем пустой интент
        if (intent == null && isRunning) {
            AppLogger.log("VPN", "Sticky restart with null intent - ignoring")
            return START_STICKY
        }

        val newConfig = intent?.getStringExtra("config") ?: ""
        if (newConfig.isEmpty()) {
            if (!isRunning) {
                AppLogger.log("VPN", "Empty config, stopping")
                stopSelf()
            }
            return START_NOT_STICKY
        }

        if (isRunning && newConfig == configJson) {
            AppLogger.log("VPN", "Already running with same config - ignoring")
            return START_STICKY
        }

        configJson = newConfig
        startVpnInternal()
        return START_STICKY
    }

    private fun startVpnInternal() {
        AppLogger.log("VPN", "Starting VPN internal")
        if (isRunning) doStop() // Перезапуск если уже был активен

        try {
            val root = org.json.JSONObject(configJson)
            val srvIp = root.optString("server_ip", "31.42.120.154")
            val srvPort = root.optInt("port", 9999)

            val builder = Builder()
            builder.setSession("Hasta-Vaquet")
            builder.setMtu(1300)
            builder.setBlocking(true)
            builder.setAlwaysOn(true)

            val cfg = parseConfig(configJson)
            builder.addAddress(cfg.internalIp, 24)
            builder.addRoute("0.0.0.0", 0)
            builder.addRoute("::", 0)  // IPv6 blackhole

            builder.addDnsServer(root.optString("dns", "1.1.1.1"))

            val allBypass = (cfg.bypassPackages + SMART_BYPASS_PACKAGES).distinct()
            for (pkg in allBypass) {
                try { builder.addDisallowedApplication(pkg) } catch (_: Exception) {}
            }

            if (android.os.Build.VERSION.SDK_INT >= android.os.Build.VERSION_CODES.R) {
                builder.setAlwaysOn(true)
            }

            tunFd = builder.establish()
            if (tunFd == null) {
                AppLogger.log("VPN", "TUN establish failed")
                stopSelf()
                return
            }

            isRunning = true
            val fd = tunFd!!.detachFd()
            AppLogger.log("VPN", "TUN established, fd=$fd")

            Thread {
                try {
                    AppLogger.log("VPN", "Go Core starting with fd=$fd and self-protector")
                    val result = Core.startVPN(configJson, fd.toLong(), this)
                    AppLogger.log("VPN", "Go Core result: $result")

                    if (result != "ok") {
                        android.os.Handler(android.os.Looper.getMainLooper()).post { doStop() }
                    }
                } catch (e: Exception) {
                    AppLogger.log("VPN", "Thread error: ${e.message}")
                    android.os.Handler(android.os.Looper.getMainLooper()).post { doStop() }
                }
            }.start()

        } catch (e: Exception) {
            AppLogger.log("VPN", "startVpnInternal error: ${e.message}")
            doStop()
        }
    }

    private fun doStop() {
        AppLogger.log("VPN", "doStop: cleaning up resources")
        isRunning = false
        Core.stopVPN()

        try {
            // Если мы использовали detachFd(), объект tunFd уже не владеет нативным дескриптором,
            // но вызов close() всё равно полезен для очистки Java-объекта.
            tunFd?.close()
            AppLogger.log("VPN", "tunFd closed")
        } catch (e: Exception) {
            AppLogger.log("VPN", "tunFd close error: ${e.message}")
        }
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
