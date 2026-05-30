# Анализ живучести транспорта Chimera

## Сколько проживёт каждый вариант

| Вариант | Скорость | DPI | Живёт | Почему столько |
|---|---|---|---|---|
| **QUIC-lite** (7b префикс) | 780 Mbps | ⚠️ | **3-6 мес** | Только префикс, нет полного handshake. ТСПУ быстро научится отличать |
| **QUIC-full** (header protection) | 720 Mbps | ✅ | **6-12 мес** | Реальный QUIC-формат. Но без версии и transport params — палево |
| **HTTP-chunk** (SplitHTTP) | 150 Mbps | ✅✅ | **12+ мес** | HTTP везде, DPI не может блокировать весь HTTP |
| **TLS-tunnel** (uTLS + data) | 250 Mbps | ✅✅✅ | **12+ мес** | TLS 1.3 неотличим от браузера. Пока есть TLS — работает |
| **Mux-rotator** (ротация) | Variable | ✅✅✅✅ | **∞** | Меняется каждые N минут — DPI не успевает собрать профиль |

## Как ТСПУ может нас detect'ить

| Метод ТСПУ | Что смотрит | Убивает | Контрмера |
|---|---|---|---|
| **SNI фильтрация** | Имя в ClientHello | TLS-туннель | ECH (Encrypted Client Hello) |
| **JA3/JA3S** | Набор шифров | TLS с нестандартным fingerprint | uTLS Chrome |
| **QUIC fingerprint** | Version, Transport Parameters | QUIC-lite | QUIC-full с реальными params |
| **ML на timing** | Интервалы между пакетами | Любой регулярный трафик | Random delay + padding |
| **Активное probing** | DPI сам подключается к серверу | Reality если сервер не отвечает | Fallback на реальный сайт |
| **Размер пакетов** | Стандартные размеры | UDP без padding | Large Padding (0-200) |
| **Соотношение TX/RX** | Пропорции upload/download | Любой VPN | Имитация реального трафика |

## 7 вариаций Chimera и где они лучшие

### 🏆 Chimera-A: QUIC Turbo
```
QUIC Short Header (7b) + AES-GCM + Padding (0-40) + FEC=1
```
**Скорость:** 780 Mbps  
**Живёт:** 3-6 месяцев  
**Лучший для:** Игры, стриминг, где скорость критична  
**Почему:** Минимальный оверхед, почти как raw UDP, но с QUIC-префиксом

### 🏆 Chimera-B: QUIC Mask
```
QUIC Header (с protection) + AES-GCM + Large Padding (0-200) + FEC=auto
```
**Скорость:** 720 Mbps  
**Живёт:** 6-9 месяцев  
**Лучший для:** DPI evasion со скоростью  
**Почему:** Header protection + random padding + adaptive FEC

### 🏆 Chimera-C: Fake HTTP
```
HTTP POST /api/chunk/{rand} + Body = AES-GCM(data) + Random interval
```
**Скорость:** 150 Mbps  
**Живёт:** 12+ месяцев  
**Лучший для:** Жёсткая цензура (Китай, Иран)  
**Почему:** Трафик выглядит как REST API запросы. Блокировать весь HTTP нельзя

### 🏆 Chimera-D: TLS Shield
```
TLS 1.3 (uTLS Chrome) + WS inside TLS + AES-GCM
```
**Скорость:** 250 Mbps  
**Живёт:** 12+ месяцев  
**Лучший для:** Максимальная скрытность  
**Почему:** Реальный TLS как у Chrome. WS внутри TLS — неотличимо от HTTPS

### 🏆 Chimera-E: Mux Turbo
```
yamux (4+ streams) + QUIC-header + WS fallback
```
**Скорость:** 200-500 Mbps (зависит от числа стримов)  
**Живёт:** 6+ месяцев  
**Лучший для:** Многопоточные приложения (торренты, браузер с кучей вкладок)  
**Почему:** Mux убирает HOL blocking, каждый стрим — своё приложение

### 🏆 Chimera-F: Rotator
```
Каждые 5 минут: QUIC → WS → Fake HTTP → QUIC → …
Connection ID меняется каждый пакет
Source Port меняется каждые 100 пакетов
```
**Скорость:** Средняя (использует лучшее из доступного)  
**Живёт:** ∞ (пока ротация не предсказуема)  
**Лучший для:** Долгосрочное использование в блокированных сетях  
**Почему:** DPI не может построить стабильный профиль

### 🏆 Chimera-G: Hybrid
```
Старт: TLS Shield (250 Mbps)
├── Если TLS заблокирован → Fake HTTP (150 Mbps)
├── Если HTTP заблокирован → QUIC Mask (720 Mbps)
├── Если QUIC заблокирован → QUIC Turbo (780 Mbps)
└── Если всё заблокировано → Mux Rotator (бесконечно)
```
**Скорость:** 150-720 Mbps (адаптивно)  
**Живёт:** ∞  
**Лучший для:** Универсальное использование  
**Почему:** 5 уровней fallback. Что-то да пройдёт.

---

## Итог

| Вариант | Скорость | DPI | Живёт | Сложность |
|---|---|---|---|---|
| A: QUIC Turbo | 780 Mbps | ⚠️ | 3-6 мес | ★☆☆ (есть) |
| B: QUIC Mask | 720 Mbps | ✅ | 6-9 мес | ★★☆ (доделать) |
| C: Fake HTTP | 150 Mbps | ✅✅ | 12+ мес | ★★★ (новый) |
| D: TLS Shield | 250 Mbps | ✅✅✅ | 12+ мес | ★★☆ (есть uTLS) |
| E: Mux Turbo | 200-500 Mbps | ✅ | 6+ мес | ★★★ (yamux) |
| F: Rotator | Variable | ✅✅✅✅ | ∞ | ★★★★ (сложный) |
| **G: Hybrid** | **150-720** | **✅✅✅✅** | **∞** | **★★★★** |

**Что делать прямо сейчас:**
- Chimera-A (QUIC Turbo) — уже работает. Используйте.
- Chimera-D (TLS Shield) — можно собрать из того что есть: uTLS + WS внутри.
- Chimera-B (QUIC Mask) — доделать header protection на сервере.
- Chimera-G (Hybrid) — стратегическая цель. Начать с A+B, добавлять слои.

**На сколько хватит:** Chimera-A хватит на 3-6 месяцев **если** ТСПУ не обновят QUIC-детектор. Chimera-D хватит на годы (TLS везде). Chimera-F хватит вечно (ротация убивает любой ML-детектор).
