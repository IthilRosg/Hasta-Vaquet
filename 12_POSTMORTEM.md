# Послесловие: Мulti-transport рефакторинг (Май 2026)

## Что было сделано

### 1. Транспортный слой (`core/transport.go`)

Создан универсальный `TransportManager` с поддержкой 4 транспортов и автоматическим fallback:

| Транспорт | Протокол | Порт | TLS | Особенность |
|---|---|---|---|---|
| **WSS** | WebSocket Secure | 443 | ✅ через Cloudflare | uTLS Chrome fingerprint, NextProtos=http/1.1 |
| **WS** | WebSocket plain | 19998 | ❌ | Напрямую к серверу, без Cloudflare |
| **QUIC** | UDP + QUIC-префикс | 19999 | ❌ | 7-байтовый Short Header перед standard-пакетом |
| **UDP** | Raw UDP | 19999 | ❌ | Оригинальный формат (HMAC+DyamicID+Nonce+AES-GCM) |

Приоритет fallback: задаётся в конфиге (`transport_priority`). По умолчанию: `["udp", "ws", "quic", "wss"]`.

### 2. WSS — переписан с нуля

**Проблема:** gorilla/websocket `DialContext` выдавал `malformed HTTP response` при подключении через Cloudflare.  
**Причина:** uTLS `HelloChrome_Auto` включал ALPN `["h2", "http/1.1"]`. Cloudflare выбирал h2, WebSocket upgrade требует HTTP/1.1.

**Решение:** `tls.Config.NextProtos = []string{"http/1.1"}` — принудительно только HTTP/1.1.

**Что ещё сделано:**
- Убран самописный `wsFramer` (ручная WebSocket фрейминг + маскировка)
- Переход на gorilla/websocket `ReadMessage`/`WriteMessage`
- Все WebSocket frame details (ping/pong, маскировка, длина) теперь корректные

### 3. QUIC-header UDP — реализован полностью

**Клиент:** `quicUDPConn.Write` добавляет 7-байтовый QUIC Short Header перед standard-пакетом:
```
[0x40 | spin] [connID:4] [packetNum:2] [standard-packet]
```
Header protection не применяется (сохраняет HMAC/DynamicID/Nonce нетронутыми).

**Сервер:** UDP-reader детектит QUIC по `byte0 & 0xC0 == 0x40`, отрезает 7 байт заголовка, передаёт inner standard-пакет в `processPacket`.

**Ответы:** сервер отвечает в standard-формате (без QUIC-заголовка). QUIC-маскировка только на upload.

### 4. ReaderLoop — переписан

**Проблема:** `SetReadDeadline` на Windows (SChannel) после таймаута убивает TLS-соединение. Любой read timeout → connection dead.

**Решение:** горутинный подход:
```go
go func() {
    n, err := conn.Read(buf)
    ch <- readResult{n, err}
}()
select {
    case r := <-ch: // обработать данные
    case <-time.After(10 * time.Second): // просто цикл
}
```

Никаких `SetReadDeadline` на conn. Таймаут только на уровне select.

### 5. Сервер — async echo

**Проблема:** echo для keep-alive писался синхронно в main loop. Если TCP-буфер заполнен, main loop блокируется, перестаёт обрабатывать входящие пакеты.

**Решение:** echo пишется в отдельной горутине:
```go
if peer.Writer != nil {
    go func(w PacketWriter, data []byte) {
        w.WritePacket(data, serverCfg.FEC)
    }(peer.Writer, enc)
}
```

### 6. GUI

- Добавлен таб Transport в настройки с выбором: Auto / WSS / WS / QUIC / UDP
- Поле CDN Domain (для WSS через Cloudflare)
- Починена ошибка импорта конфигов (пустой profile_name)
- DoConnect принимает transport + cdnDomain параметры

### 7. DoH

Cloudflare Worker для DNS-over-HTTPS. Развёрнут на `doh.pybyse.airydeck.su`. Деплой через wrangler, Route через Cloudflare API.

---

## Что НЕ получилось

### 1. WSS через Cloudflare — обрывает соединение

**Симптом:** после ~8 WebSocket сообщений Cloudflare разрывает соединение (`close 1006 abnormal closure`).  
**Воспроизводится:** независимо от задержки между сообщениями (1.5с, 3с, 5с — одинаково).  
**Причина:** Cloudflare Free план имеет ограничения на WebSocket прокси. Точный лимит не документирован, но эмпирически — ~8-10 сообщений.

**Попытки решения:**
- Собственный WebSocket фреймер (`wsFramer`) — не помогло
- Async echo на сервере — не помогло
- Gorilla/websocket вместо самописного — не помогло
- Задержка между сообщениями — не помогло

**Вывод:** проблема на стороне Cloudflare, не в коде.

### 2. Скорость WS ниже UDP

**WS:** ~192 Mbps  
**UDP:** ~320 Mbps  

**Причина:** WebSocket фрейминг (2-14 байт заголовка + маскировка 4 байта на каждое сообщение). Для пакетов MTU 1300 это ~1.5% оверхеда, но gorilla/websocket добавляет внутреннюю буферизацию и синхронизацию.

### 3. QUIC-header медленнее ожидаемого

**QUIC:** ~216 Mbps (vs UDP 320 Mbps)

**Причина:** QUIC-заголовок не оптимизирован — header protection отключён, но каждый пакет копируется (append). Можно ускорить, используя `encodeQUIC` напрямую, но это требует доступа к CipherPack из transport layer.

### 4. Скорость упирается в сервер

**Сервер:** Intel Xeon Gold 6150, 350 Mbps download  
**Клиент через UDP:** 320 Mbps (93% от сервера)

Протокол не является узким местом. Для роста скорости нужен более мощный сервер или аплинк.

---

## Варианты решений

### Для WSS через Cloudflare

**Вариант A — Cloudflare Spectrum ($10/мес):**
- Проксирует любые TCP/UDP протоколы, не только HTTP/WS
- Нет лимита на WebSocket сообщения
- IP сервера скрыт

**Вариант B — Xray Reality на сервере (бесплатно):**
```
[Клиент] → Xray Reality (TLS как Chrome → microsoft.com) → [Hasta-Vaquet Server]
```
- Абсолютная скрытность от DPI
- Никаких лимитов на сообщения
- Настройка ~15 минут
- Reality не требует сертификата на сервере

**Вариант C — BoringTun + Cloudflare Warp:**
- WireGuard через Cloudflare Warp как апстрим
- Hasta-Vaquet → Warp → Internet
- Warp бесплатен, скорость ~200 Mbps

### Для скорости

**Вариант A — BBR congestion control:**
Добавить контроль перегрузки BBR на сервере:
```bash
sysctl -w net.core.default_qdisc=fq
sysctl -w net.ipv4.tcp_congestion_control=bbr
```

**Вариант B — MTU tuning:**
Текущий MTU 1300. Можно поднять до 1400-1450, если нет фрагментации. Больше MTU = меньше оверхеда.

**Вариант C — Multi-writer:**
Сейчас один writerLoop на соединение. Можно распараллелить на 2-4 writer'а (UDP socket с SO_REUSEPORT).

### Для QUIC

**Вариант A — сквозной QUIC:**
Передать CipherPack в quicUDPConn, использовать `encryptQUIC` напрямую вместо оборачивания standard-пакета. Ответы от сервера тоже в QUIC-формате.

**Вариант B — полный quic-go:**
Заменить `net.DialUDP` на `quic-go`. Трафик неотличим от реального QUIC. Но +1 зависимость и больше complexity.

---

## Итог

**Работает:**
- UDP: 320 Mbps, 46ms ping, стабильно
- WS (direct): 192 Mbps, 47ms ping, стабильно
- QUIC-header: 216 Mbps, 52ms ping, стабильно
- DoH: работает через Cloudflare Worker
- Сервер: QUIC-детекция, async echo, NAT

**Требует доработки:**
- WSS через Cloudflare — упирается в лимиты бесплатного тарифа
- QUIC — неполный (только upload, standard ответы)
- Скорость — упирается в сервер (350 Mbps)

**Рекомендация:** использовать **UDP** для максимальной скорости, **WS** если UDP заблокирован, **QUIC** для маскировки.
