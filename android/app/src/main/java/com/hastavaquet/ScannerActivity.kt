package com.hastavaquet

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Bundle
import android.util.Size
import android.widget.Toast
import androidx.activity.ComponentActivity
import androidx.activity.result.contract.ActivityResultContracts
import androidx.camera.core.*
import androidx.camera.lifecycle.ProcessCameraProvider
import androidx.camera.view.PreviewView
import androidx.core.content.ContextCompat
import com.google.mlkit.vision.barcode.BarcodeScanning
import com.google.mlkit.vision.barcode.common.Barcode
import com.google.mlkit.vision.common.InputImage
import java.util.concurrent.Executors

class ScannerActivity : ComponentActivity() {

    private val cameraExecutor = Executors.newSingleThreadExecutor()
    private var barcodeScanner = BarcodeScanning.getClient()
    private lateinit var previewView: PreviewView

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        previewView = PreviewView(this).apply {
            scaleType = PreviewView.ScaleType.FILL_CENTER
        }
        setContentView(previewView)

        if (checkSelfPermission(Manifest.permission.CAMERA) == PackageManager.PERMISSION_GRANTED) {
            startCamera()
        } else {
            requestPermissionLauncher.launch(Manifest.permission.CAMERA)
        }
    }

    private val requestPermissionLauncher = registerForActivityResult(
        ActivityResultContracts.RequestPermission()
    ) { granted ->
        if (granted) startCamera()
        else { Toast.makeText(this, "Нужна камера", Toast.LENGTH_LONG).show(); finish() }
    }

    private fun startCamera() {
        val cameraProviderFuture = ProcessCameraProvider.getInstance(this)
        cameraProviderFuture.addListener({
            val cameraProvider = cameraProviderFuture.get()

            val preview = Preview.Builder().build().also {
                it.setSurfaceProvider(previewView.surfaceProvider)
            }

            val imageAnalysis = ImageAnalysis.Builder()
                .setTargetResolution(Size(1280, 720))
                .setBackpressureStrategy(ImageAnalysis.STRATEGY_KEEP_ONLY_LATEST)
                .build()
                .also { it.setAnalyzer(cameraExecutor) { image -> processImage(image) } }

            cameraProvider.unbindAll()
            cameraProvider.bindToLifecycle(
                this, CameraSelector.DEFAULT_BACK_CAMERA, preview, imageAnalysis
            )
        }, ContextCompat.getMainExecutor(this))
    }

    private fun processImage(image: ImageProxy) {
        val mediaImage = image.image
        if (mediaImage != null) {
            val input = InputImage.fromMediaImage(mediaImage, image.imageInfo.rotationDegrees)
            barcodeScanner.process(input)
                .addOnSuccessListener { barcodes ->
                    for (barcode in barcodes) {
                        val value = barcode.rawValue ?: continue
                        if (barcode.valueType == Barcode.TYPE_TEXT && (
                            value.contains("server_ip") || value.contains("secret_key")
                        )) {
                            setResult(RESULT_OK, Intent().putExtra(EXTRA_CONFIG, value))
                            finish()
                            return@addOnSuccessListener
                        }
                    }
                }
                .addOnCompleteListener { image.close() }
        } else {
            image.close()
        }
    }

    override fun onDestroy() {
        super.onDestroy()
        cameraExecutor.shutdown()
    }

    companion object {
        const val REQUEST_CODE = 2000
        const val EXTRA_CONFIG = "config"
    }
}
