package com.hastavaquet

import android.content.Context
import java.io.File
import java.io.FileWriter
import java.text.SimpleDateFormat
import java.util.*

object AppLogger {
    private var writer: FileWriter? = null
    private val sdf = SimpleDateFormat("HH:mm:ss.SSS", Locale.US)

    fun init(context: Context) {
        val dir = context.getExternalFilesDir(null) ?: context.filesDir
        val file = File(dir, "live_logs.txt")
        writer = FileWriter(file, true)
        log("APP", "=== Hasta-Vaquet started ===")
    }

    fun log(tag: String, message: String) {
        val ts = sdf.format(Date())
        val line = "$ts [$tag] $message\n"
        writer?.write(line)
        writer?.flush()
        android.util.Log.i(tag, message)
    }

    fun close() {
        writer?.close()
        writer = null
    }
}
