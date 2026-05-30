# Phase 9: Multi-Transport & Hardening (Май 2026)

> Живой документ. Обновляется каждый шаг.

## Текущее состояние (2026-05-30)

**Билд:** ✅ Windows GUI (Wails2 + Svelte)  
**Сервер:** ✅ 45.134.39.18:4433 (UDP), :4434 (WS)  
**Xray Reality:** ✅ :443 (VLESS + XTLS + Vision)  
**DoH:** ✅ doh.pybyse.airydeck.su  
**GitHub:** ✅ github.com/IthilRosg/Hasta-Vaquet

---

## Транспорты

| Транспорт | Порт | Статус | Скорость | DPI |
|---|---|---|---|---|
| **Raw UDP** | 4433 | ✅ | **880 Mbps** | ❌ |
| **QUIC-header UDP** | 4433 | ✅ | **819 Mbps** | ✅ QUIC Short Header |
| **WS (plain)** | 4434 | ✅ | 221 Mbps | ⚠️ |
| **UDP + Large Padding** | 4433 | ✅ | 784 Mbps | ⚠️ Случайный размер |
| WSS (Cloudflare) | — | ❌ Удалён | — | Cloudflare лимиты |
| WS+Mux (yamux) | — | ❌ Нужен сервер | — | — |
| XHTTP (chunked) | — | ❌ Не реализован | — | — |

## Протокол

**Wire Format (Phase 6):**
```
[HMAC(4)] [DynamicID(2)] [Nonce(12)] [AES-256-GCM(inner)]
```

**QUIC-header поверх:**
```
[0x40|spin(1)] [connID(4)] [packetNum(2)] [standard-packet]
```

**Шифрование:** AES-256-GCM, per-user ключи  
**Padding:** 0-40 (стандарт), 0-200 (Large Padding)  
**FEC:** 1-5x (1=off, 2-5 избыточность)  
**MTU:** 1300  

## Оптимизации (май 2026)

| Что | Было | Стало |
|---|---|---|
| Mutex в CipherPack | sync.Mutex | Per-goroutine (отдельные enc/dec/ping CP) |
| PRNG | math/rand (блокировки) | fastPRNG (xorshift64*, 10x быстрее) |
| Буферы | make([]byte,N) на пакет | sync.Pool (0 аллокаций) |
| authData/marker | heap alloc | stack alloc |
| QUIC header | 3 аллокации | 1 аллокация |
| ReaderLoop | SetReadDeadline (SChannel баг) | goroutine-based, без deadline |
| Echo writes | sync (блокировал main loop) | async goroutine |

## Сервер

**Характеристики:** Intel Xeon Gold 6150, 4 ядра, 15GB RAM, 350 Mbps аплинк  
**Сервисы:** hasta-vaquet (UDP :4433, WS :4434), Xray Reality (:443), Caddy (:4443)

**Xray Config:**
```json
{
  "dest": "www.microsoft.com:443",
  "serverNames": ["www.microsoft.com", "www.bing.com", "www.cloudflare.com", "www.github.com"],
  "flow": "xtls-rprx-vision"
}
```

## GUI (Windows)

**Фреймворк:** Wails v2.12.0 + Svelte + TypeScript  
**Окно:** 480x700, тёмная тема  
**Транспорты в настройках:** Auto / WSS / WS / QUIC / UDP  
**Профили:** импорт/экспорт JSON, список, авто-загрузка  
**Статус:** connection state, TX/RX speed, ping, loss, uptime

## DoH

**URL:** https://doh.pybyse.airydeck.su/dns-query  
**Бэкенд:** Cloudflare Worker  
**Rate limit:** 200 req/min per IP  
**Fallback:** 1.1.1.1 → 8.8.8.8

---

## Ближайшие задачи

### Phase 9a: XHTTP transport
- HTTP chunked transport (каждый чанк = POST запрос)
- Серверный handler на :4435
- Ожидаемая скорость: ~150 Mbps, макс DPI evasion

### Phase 9b: Mux на сервере
- yamux server-side для WS
- Мультиплексирование потоков
- Тест многопоточности

### Phase 9c: Chimera Final
- Adaptive переключение между UDP/QUIC/WS
- Авто FEC при потерях
- Protocol rotation каждые N минут

### Phase 9d: Production
- Rate limiting на сервере
- expires_at + quota_bytes
- Windows Installer (Inno Setup)
- Systemd unit update

---

## Документация

| Файл | Описание |
|---|---|
| `00_MASTER_PLAN.md` | Стратегический план |
| `12_POSTMORTEM.md` | Послесловие: что сделано, что нет |
| `docs/transport-strategy.md` | Стратегия транспортов |
| `docs/chimera-analysis.md` | 7 вариаций Chimera |
| `docs/state-of-art-2026.md` | Современные транспорты (quic-go, Hysteria) |
| `docs/reality-dest-analysis.md` | Анализ Reality destinations |
| `docs/bench-mutations-results.md` | Результаты тестов мутаций |
| `docs/transports-2026.md` | XHTTP, Mux, SplitHTTP, gRPC, MASQUE |
| `docs/cdn-deploy.md` | Развёртывание за Cloudflare |
| `docs/doh-worker.js` | Cloudflare Worker DoH |
| `docs/performance-tuning.md` | Настройка скорости |

## Команды

```sh
# Сборка сервера (Linux)
GOOS=linux GOARCH=amd64 go build -o server/hasta-vaquet-server -ldflags="-s -w" ./server/...

# Сборка GUI (Windows)
cd gui && wails build -o Hasta-Vaquet-new.exe

# Запуск тестов
go test ./... -v

# Вет
go vet ./...
```
