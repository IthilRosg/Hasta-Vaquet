# Hasta-Vaquet VPN — Briefing для новой сессии

## Проект

Кастомный VPN-протокол (Phase 6) для обхода DPI/ТСПУ в РФ.
Язык: Go 1.26, Windows клиент (Wails2+Svelte), Linux сервер.

**Сервер:** 45.134.39.18 (SSH ключи установлены)
| Порт | Назначение |
|---|---|
| 4433 UDP | Hasta-Vaquet протокол (UDP / QUIC-header) |
| 4434 TCP | WebSocket транспорт |
| 4435 TCP | XHTTP транспорт (batched POST) |
| 443 TCP | Xray Reality (отдельный сервис) |
| 9998 TCP | Web admin panel (localhost) |

## Архитектура

```
protocol/         — крипто: AES-256-GCM, HMAC, DynamicID, padding
core/             — VPN движок + транспорты (UDP/QUIC/WS/XHTTP)
server/           — Linux сервер: TUN, multi-user, web panel
gui/              — Windows GUI (Wails2 + Svelte)
cmd/bench/        — бенчмарк (28 вариаций, топ-3)
cmd/echotest/     — диагностика keep-alive echo
```

## Wire Format (Phase 6)
```
[HMAC(4)] [DynamicID(2)] [Nonce(12)] [AES-256-GCM(inner)]
inner = [real_len(2)] [data] [random_padding]
```
- HMAC bit6 принудительно = 1 (QUIC mask)
- DynamicID = ShortID ^ FNV-1a(RoutingSalt + Nonce)
- Per-user ключи, stateless, Bloom filter replay protection

## Транспорты

| Транспорт | Порт | Скорость | DPI |
|---|---|---|---|
| UDP raw | 4433 | ~1100 Mbps | низкая |
| QUIC-header | 4433 | ~1025 Mbps | средняя (7b префикс) |
| WS | 4434 | ~270 Mbps | средняя (HTTP Upgrade) |
| XHTTP | 4435 | ~85 Mbps | высокая (простой HTTP POST) |

## Топ-3 обфускация + скорость
1. QUIC-Pad200 (QUIC + padding 200) — 1025 Mbps
2. QUIC-Pad512 — 1023 Mbps
3. QUIC-FEC2 — 527 Mbps

## Режимы маршрутизации (Smart Bypass)

| Режим | Описание | CIDR лист |
|---|---|---|
| **Off** | Весь трафик через VPN | — |
| **Bypass** | CIDRs идут напрямую, остальное через VPN | Банки РФ, игры |
| **VPN Only** | Только CIDRs через VPN, остальное напрямую | Антифильтр, игры |

### Пресеты
- **Russian Banks**: 27 CIDR (Сбер, ВТБ, Тинькофф, Альфа, Газпром, Госуслуги, ФНС, ЦБ и др.)
- **Game Anti-VPN**: Cloudflare, Fastly, AWS (для игр блокирующих VPN — Valorant, CS2)
- **AntiFilter**: скачивает allyouneed.lst (antifilter.download), агрегирует /24→/16

## Последние изменения (коммиты)
1. **Echo fix**: WireSock kernel driver перехватывал UDP. Отключён через `sc config wiresock start=disabled`, ребут.
2. **WS порт**: Hardcoded 19998 → cfg.WSPort (4434)
3. **padBufPool**: 256B zero-filled → 65535B crypto/rand
4. **XHTTP транспорт**: batched POST (500 пакетов/запрос). Echo ✅, 85 Mbps.
5. **Smart Bypass**: BypassMode + BypassCIDRs + VPNCIDRs. Три режима.
6. **AntiFilter**: загрузка allyouneed.lst, агрегация /24→/16
7. **Game presets**: Cloudflare/Fastly/AWS CIDRs
8. **GUI**: Settings gear 18px→24px, Bypass tab с пресетами

## Что НЕ сделано (todo)
- **QUIC-full (CipherModeQUIC)**: на сервере `IsQUIC` никогда не устанавливается — dead code
- **yamux / mux**: зависимость в go.mod, в коде нет
- **quic-go**: настоящий QUIC (RFC 9000) вместо префикса — ~400 Mbps
- **Kill Switch Android**: не реализован
- **Android статистика в UI**: `GetStats()` не интегрирована
- **Rate limiting на сервере**: нет
- **Forward secrecy**: ключи статические
- **CI/CD**: всё ручное
- **Installer**: Inno Setup не сделан
- **GUI профили**: быстрые переключатели на главном экране

## Ключевые баги (внимание!)
- **encryptQUIC()**: padBufPool фикс был только для standard mode, в QUIC-mode тот же паттерн
- **QUIC key reuse**: protocol/quic.go — один ключ для GCM и header protection
- **Server bloom reset**: два тикера (60s + 5s) могут триггернуть двойной сброс
- **main.go**: `//go:build ignore` — dead code

## Правила работы
- Token Efficiency (AGENTS.md): минимум флаффа, root cause first, exact errors
- Caveman mode: short, fragments OK, no filler
- Изменения: minimal diff, batch parallel edits
- Тестирование: `go build ./... && go vet ./...` перед деплоем
- Деплой: `cd gui && go build ./...`, SCP server binary, `systemctl restart`
- Тесты: `go test ./... -v` (crypto_test.go, integration_test.go)
