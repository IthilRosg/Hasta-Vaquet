# Phase 8: Android Client — журнал разработки

> Живой документ. Обновляется каждый шаг.

## Текущее состояние (2026-05-17 12:15) — Phase 8 БАЗОВАЯ РАБОТОСПОСОБНОСТЬ ДОСТИГНУТА

**Билд:** ✅  
**Сервер:** ✅ 31.42.120.154:9999  
**QR-сканер:** ✅ камера работает (PreviewView + ImageAnalysis + ML Kit)  
**Профили:** ✅ CRUD SharedPreferences, дропдаун  
**Disconnect:** ✅ останавливает Core + сервис, значок VPN исчезает (fix: `tunFd.close()`)  
**UDP-сокет:** ✅ Go-managed + Protector (fix: Android 15 reflection block)  
**protect():** ✅ работает через callback из Go в Kotlin  
**TUN:** ✅ устанавливается, IPv6 Blackhole добавлен (fix: routing leak)  
**Трафик:** ✅ подтверждён логами (writerLoop/readerLoop active)

**Все критические баги закрыты. Статистика ✅ Uptime ✅ Статус ✅ VPN ✅ Сканер ✅
- [ ] Phase 8.1: Интеграция реальной статистики в UI (Poll GetStats)
- [ ] Phase 8.2: Реализация "Kill Switch" на уровне Android

## Найденные и исправленные баги

| Баг | Симптом | Причина | Исправление |
|---|---|---|---|
| `EAGAIN` спам | writerLoop: TUN read error | fd в non-blocking режиме, syscall.Read не ждёт | `os.File.Read()` через runtime poller |
| `NetworkOnMainThreadException` | UDP socket ERROR: null | `DatagramSocket.connect()` на main thread | `Thread{...}.start()` |
| `fromDatagramSocket() → null` | ERROR: NullPointerException | API возвращает null на Xiaomi | Reflection: `impl.fd` → `ParcelFileDescriptor.dup()` → `detachFd()` |
| `protect()` не работал | UDP через TUN → петля | Сокет не защищён от VPN-маршрутизации | `protect(udpSocket)` + `protect(int fd)` |
| PreviewView GONE | Камера не включалась | GONE убирает View из layout → surface provider мёртв | Full-screen PreviewView (стандартная схема) |
| `onclick` с JSON.stringify | Кнопки не работали в web-панели | Двойные кавычки ломали HTML-парсер | Data-атрибуты + event delegation |
| `short_id` как строка | "invalid json" | JS отправлял "003", Go ждал uint16 | `parseInt(shortId, 10)` |
| Профиль удалялся — коннект оставался | Фантомное подключение | `loadConfig()` → `LoadDefaultConfig()` восстанавливал настройки | `SaveLastProfile('')` + без `loadConfig()` |
| Uptime не обновлялся | 00:00 навсегда | Обновление только при трафике | `setInterval` каждую секунду |

## Архитектура Android-клиента

```
┌─ MainActivity ──────────────────────────────────────────┐
│  SharedPreferences: профили (CRUD)                       │
│  onConnect → startVpn(config)                            │
│    ↓                                                     │
│  VpnService.prepare() → разрешение пользователя          │
│    ↓                                                     │
│  startForegroundService(VpnService, config)              │
└──────────────────────────────────────────────────────────┘
                         ↓
┌─ HastaVaquetVpnService ─────────────────────────────────┐
│  1. Builder.setMtu(1300).addRoute("0.0.0.0",0)          │
│  2. addDisallowedApplication(pkg) — Smart Bypass        │
│  3. establish() → detachFd() → TUN fd                   │
│  4. Thread:                                              │
│     - DatagramSocket().connect(server)                   │
│     - protect(socket)                                    │
│     - reflection: impl.fd → dup → detach → UDP fd       │
│     - Core.startVPN(configJson, tunFd, udpFd)            │
│  5. onRevoke/doStop → Core.stopVPN() + stopForeground   │
└──────────────────────────────────────────────────────────┘
                         ↓
┌─ core (Go, gomobile) ───────────────────────────────────┐
│  gomobile.go: StartVPN(config, tunFd, udpFd)            │
│    plat.tunFile = os.NewFile(tunFd, "tun")               │
│    plat.protectedConn = os.NewFile(udpFd, "udp")         │
│    vpn.Start() →                                          │
│      openTunnel: net.FileConn(protectedConn) → UDPConn   │
│      keepAliveLoop, readerLoop, writerLoop, statsLoop    │
│                                                          │
│  vpn_android.go:                                         │
│    openTunnel: net.FileConn → защищённый UDP             │
│    readerLoop: UDP Read → Decrypt → TUN Write            │
│    writerLoop: TUN Read → Encrypt → UDP Write            │
│    closeTunnel: закрыть оба fd                           │
└──────────────────────────────────────────────────────────┘
```

## Ключевые пути

```
android/app/src/main/java/com/hastavaquet/
├── MainActivity.kt            — точка входа, профили, лаунчеры
├── HastaVaquetVpnService.kt   — VpnService, TUN + UDP socket
├── ScannerActivity.kt         — CameraX + ML Kit QR
├── AppLogger.kt               — логгер в файл + logcat
└── ui/
    ├── ConnectScreen.kt       — Compose UI (кнопка, карточки, профили)
    └── Theme.kt               — тёмная тема

core/
├── vpn.go                     — общая логика (StatusListener, циклы)
├── vpn_windows.go             — Windows: Wintun + netsh
├── vpn_android.go             — Android: os.File TUN + protected UDP
├── gomobile.go                — StartVPN/StopVPN/GetStats
├── crypto.go                  — AES-GCM, HMAC, DynamicID
└── config.go                  — Config, LoadConfig
```

## Команды

```sh
# Сборка AAR
cd core && gomobile bind -target android -androidapi 35 -o ../android/app/libs/core.aar hasta-vaquet/core

# Логи с устройства
cd android/logs && capture_logs.bat     # весь logcat → hastavaquet_live.txt

# Ядерная очистка кеша
cd android && clean_build.bat

# Логи Go в logcat
adb logcat -s GoLog:V
```
