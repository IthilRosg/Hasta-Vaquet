# Стратегия транспортов — май 2026

## 1. Что для чего

### 🎮 Для игр (низкая задержка, минимум джиттера)
| Транспорт | Пинг | Потери | Скорость |
|---|---|---|---|
| **Raw UDP** | **46ms** | 0% | **810 Mbps** |
| UDP + Large Padding | 48ms | 0% | 784 Mbps |
| QUIC-header UDP | 50ms | 0% | 770 Mbps |
| WS | 47ms | 0% | 189 Mbps |

**Вердикт:** Raw UDP — лучший для игр. Минимальный оверхед, пакеты не фрагментируются (MTU 1300). Large Padding добавляет 2ms — незаметно, но скрывает размер пакетов от DPI.

### 🛡 Для DPI evasion (РФ, Китай)
| Транспорт | Маскировка | Скорость |
|---|---|---|
| **QUIC-header UDP** | **QUIC Short Header** | **770 Mbps** |
| Reality (через Xray) | TLS 1.3 → microsoft.com | ~250 Mbps |
| WS | HTTP Upgrade | 189 Mbps |
| Raw UDP | Нет | 810 Mbps |

**Вердикт:** QUIC-header UDP — лучший баланс. 770 Mbps, пакет выглядит как QUIC для DPI. Reality даёт 100% маскировку, но через Xray-core (тяжёлый).

### 📥 Для загрузок / стриминга
| Транспорт | Скорость | Надёжность |
|---|---|---|
| **Raw UDP** | **810 Mbps** | Потери >5% → FEC |
| QUIC-header | 770 Mbps | Авто-FEC если надо |
| WS | 189 Mbps | TCP — нет потерь |

### 🔄 Если UDP заблокирован
| Транспорт | Скорость | 
|---|---|
| **WS** | **189 Mbps** |
| Reality + WS | ~150 Mbps (TLS overhead) |

---

## 2. Свой транспорт-химера «Hasta»

Идея: скрестить **QUIC-формат** (DPI видит QUIC), **uTLS** (Chrome fingerprint), **yamux** (мультиплексирование), **padding** (случайный размер) и **Brotli** (сжатие заголовков).

### Схема пакета

```
[QUIC Short Header 7b]  — 0x40, ConnectionID, PacketNumber
[Frame Type 1b]          — DATA / STREAM / PING / PADDING
[Stream ID 2b]           — для Mux (0 = одиночный)
[Payload Length 2b]      — длина полезных данных
[AES-GCM Payload]        — зашифрованный VPN-пакет
[Padding 0-255b]         — случайный мусор
```

### Компоненты

| Компонент | Берём | Статус |
|---|---|---|
| QUIC Short Header | protocol/quic.go | ✅ |
| uTLS Chrome fingerprint | protocol/tls.go | ✅ |
| Mux (yamux) | github.com/hashicorp/yamux | ✅ добавлен |
| AES-256-GCM | protocol/cipher.go | ✅ |
| Random Padding | protocol/cipher.go | ✅ |
| **Brotli сжатие заголовков** | Новое | ❌ |
| **Protocol Rotation** | Новое | ❌ |
| **Adaptive FEC** | core/vpn.go | ❌ |

### Режимы работы

| Режим | Скорость | DPI | Когда |
|---|---|---|---|
| **Hasta-QUIC** | ~780 Mbps | ✅ | UDP не блокирован |
| **Hasta-Mux** | ~200 Mbps | ✅ | Нужна многопоточность |
| **Hasta-WS** | ~190 Mbps | ⚠️ | UDP заблокирован |
| **Hasta-Reality** | ~250 Mbps | ✅✅ | Макс скрытность |

### Adaptive transport

Автоматическое переключение между режимами:

```
Старт → Hasta-QUIC
  ├── Если потери > 5% → Hasta-QUIC + FEC=2
  ├── Если UDP не отвечает 5с → Hasta-WS
  └── Каждые N минут → ротация Connection ID
```

---

## 3. План реализации

### Фаза 1: Hasta-QUIC (1 день)
- Улучшить QUIC-заголовок (добавить Frame Type, Stream ID, Padding)
- Научить сервер отвечать в том же формате (сейчас отвечает в standard)
- Замерить скорость

### Фаза 2: Hasta-Mux (1-2 дня)
- yamux поверх WS на сервере
- yamux поверх UDP-сессии (ack-based mux)
- Тест многопоточности

### Фаза 3: Adaptive + Rotation 
- Мониторинг потерь и задержки
- Авто-переключение транспорта
- Ротация Connection ID / Source Port каждые N пакетов

---

## 4. Итоговая рекомендация

**Прямо сейчас используйте:** UDP + Large Padding (784 Mbps, 46ms, случайный размер пакетов) — лучший баланс скорости и скрытности.

**Если хотите максимум:** реализовать Hasta-QUIC с полным циклом (клиент → QUIC → сервер → QUIC → клиент). Это даст 780+ Mbps с маскировкой под реальный QUIC, неотличимый для ТСПУ.
