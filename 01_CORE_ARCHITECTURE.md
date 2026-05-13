# Core Architecture Rules

- **Язык:** Чистый Go.
- **Сетевой протокол:** UDP (порт задаётся через `-port` флаг или `config.json`; дефолт 9999).
- **Конфигурация:** Параметры подключения (ServerIP, Port, SecretKey) задаются через CLI-флаги (`-server`, `-port`, `-key`) либо читаются из `config.json`. Приоритет: флаги > config.json > хардкод-дефолты.
- **Keep-Alive:** Клиент отправляет зашифрованный пакет нулевой длины через случайный интервал 10-30 секунд для предотвращения закрытия NAT-таймаутов. Сервер распознаёт keep-alive по `len(decrypted) == 0`, логирует и отбрасывает (не пишет в TUN).
- **Phase 5 Wire Format:**
  ```
  [HMAC-SHA256(Key, Nonce)[:4] | QUIC_mask] [Nonce 12 байт] [AES-GCM(payload + padding)]
  ```
  - Первый байт HMAC-маркера: бит 6 принудительно установлен (QUIC fixed bit mask)
  - Сервер при проверке снимает бит 6 с marker И expected перед `hmac.Equal`
- **Phase 6 Wire Format (с 2026-05-13):**
  ```
  [HMAC(4)] [DynamicID(2)] [Nonce(12)] [AES-GCM(payload)]
  ```
  - DynamicID = ShortID ^ FNV-1a(RoutingSalt + Nonce)[:2]
  - Сервер декодирует ShortID за O(1), извлекает per-user ключ, проверяет HMAC
  - Динамические 2 байта меняются на каждом пакете — DPI не может сгруппировать пакеты одного клиента
- **Multi-User:** Маршрутизация через `map[uint16]*Peer` и `map[string]*Peer` (по dstIP). Каждый клиент — свой ShortID, свой SecretKey, своя маскировка.
- **Native UDP Echo Ping:** Keep-Alive клиента → сервер отвечает 1-байтовым шифрованным эхо → клиент замеряет RTT. Потери считаются как `(echoSent - echoAcked) / echoSent`. Никакого TCP/ICMP/внешних серверов.
- **IPv6 Blackhole:** Маршрут `::/0` направляется через Wintun-адаптер. Наш код игнорирует IPv6 (packet[0]>>4 != 4) → трафик падает в чёрную дыру, предотвращая IPv6 утечку.
- **UI (Phase 7b):** Wails + Svelte. Асинхронный мост Go↔JS через runtime.EventsEmit. HideWindow для всех exec.Command. Профили через выпадающий список с last_profile.txt.
- **Разделение сред:**
    - Клиент (Windows): использовать `golang.org/x/sys/windows` и `wintun`. Сборка только под Windows.
    - Сервер (Linux): использовать стандартный `os.OpenFile("/dev/net/tun", ...)`. Сборка только под Linux.
- **Запреты:**
    - НИКАКИХ Windows-библиотек в серверном коде (вызывает ошибки кросс-компиляции).
    - НИКАКИХ захардкоженных путей, кроме `/dev/net/tun` на Linux.