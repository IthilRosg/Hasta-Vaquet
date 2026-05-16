# Phase 8: Android Client — журнал разработки

> Живой документ. Обновляется каждый шаг, чтобы не терять контекст.

## Текущее состояние (2026-05-16)

**Билд:** ✅ проходит  
**Сервер:** ✅ работает, 31.42.120.154:9999, пользователи Alice(#1) Bob(#2) Android(#13)  
**AAR:** ✅ core.aar собран, в android/app/libs/  

**Не работают:**
- [ ] **VPN-соединение** — VpnService стартует, `Core.startVPN() → "ok"`, но трафика нет  
- [ ] **QR-сканер** — CameraX стартует, Preview есть, но ML Kit не сканирует (или сканирует, но не отображает результат)

## Архитектура Android

```
MainActivity → VpnService → TUN fd → gomobile → core (Go)
                     ↑                   ↓
              addDisallowedApp    UDP ↔ сервер 31.42.120.154:9999
              (Smart Bypass)
```

Ключевые файлы:
- `android/.../HastaVaquetVpnService.kt` — VpnService, establish() → detachFd() → Go
- `android/.../MainActivity.kt` — профили (SharedPreferences), лаунчеры
- `android/.../ScannerActivity.kt` — CameraX + ML Kit
- `core/vpn_android.go` — platform hooks: syscall.Read/Write fd
- `core/gomobile.go` — StartVPN/StopVPN/GetStats
- `core/vpn.go` — общая логика (StatusListener, keepAlive, stats)

## Отладка

Логи Go в Android доступны через `adb logcat -s HastaVaquet:V GoLog:V`.
Включены в `vpn_android.go`: лог каждого UDP-пакета, ошибок read/write.

## Последние изменения

- `vpn_android.go`: добавлен `net.DialUDP` в openTunnel (был nil conn!)  
- `gomobile.go`: убран `os.NewFile()` (GC закрывал fd)  
- `ScannerActivity`: добавлен PreviewView 1×1 (CameraX требует Preview)  
- `MainActivity`: профили SharedPreferences, VPN-лаунчер через ActivityResultContracts
- `MainActivity`: deprecated `startActivityForResult` → `registerForActivityResult`
