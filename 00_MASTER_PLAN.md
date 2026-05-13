# Master Plan: Hasta-Vaquet VPN Ecosystem

## 1. Vision
Создание независимой, высокопроизводительной и устойчивой к DPI (Deep Packet Inspection) экосистемы VPN. 
- **Транспорт:** Чистый UDP с кастомным криптографическим протоколом.
- **Безопасность:** AES-256-GCM, защита от replay-атак, криптографическая обфускация.
- **Клиенты:** Windows (нативный Wintun), Android (VpnService), CLI.
- **Сервер:** Stateless Linux-шлюз с мультиплексированием клиентов и Web-панелью управления.

## 2. Roadmap

- [x] **Phase 1: Proof of Concept.** Базовый UDP-туннель, передача сырых IP-пакетов, проверка связности.
- [x] **Phase 2: Core Security.** Внедрение AES-256-GCM, решение проблемы фрагментации (префикс длины), MTU 1300.
- [x] **Phase 3: Global Routing.** Заворачивание 0.0.0.0/0 на Windows через Wintun, исключение петли маршрутизации, перехват DNS.
- [x] **Phase 4: Operationalization & Stability.** Избавление от хардкода (config.json / CLI флаги), внедрение Keep-Alive (анти-таймаут NAT), агрегированное логирование (30-сек интервал), корректное завершение работы (очистка маршрутов).
- [x] **Phase 5: DPI Evasion (ЗАВЕРШЕНА).** In-band signaling через HMAC-SHA256-маркер. QUIC-маска заголовка (бит 6). Динамический паддинг (Nonce-ротация: Client 0-40, Server Medium/Chaos). Bloom Filter анти-Replay (64KB, 3×FNV-1a). Keep-Alive jitter 10-30с. Асимметричное логирование клиент/сервер. Фиксация: 90 Mbps throughput, 0% packet loss.
- [x] **Phase 6: Multi-User Architecture (ЗАВЕРШЕНА).** Dynamic XOR Routing: ShortID ^ FNV-1a(RoutingSalt+nonce)[:2]. Wire Format V6 с DynamicID. O(1) маршрутизация через map[uint16]*Peer. Per-user HMAC+AES ключи. TUN dstIP → ipToPeer. Композитный Bloom фильтр (ShortID+Nonce). server_config.json с массивом users. Поддержка 1000+ клиентов.
- [x] **Phase 7b: Windows GUI (ЗАВЕРШЕНА).** Wails + Svelte. Рефакторинг ядра в core/ (config, crypto, vpn). Мост app.go: Connect/Disconnect/ImportConfig. Стеклянные карточки статистики (2x2 Grid: SPEED, DATA, NETWORK, UPTIME). Профили через dropdown. HideWindow для всех exec.Command. Native UDP Echo Ping (keep-alive echo от сервера). IPv6 blackhole (::/0 через Wintun). Встроенный wintun.dll. Скорость ~90 Mbps.
- [ ] **Phase 7c: Android Client (ТЕКУЩАЯ СТАДИЯ).** Разработка Android-клиента через Go Mobile + VpnService.
- [ ] **Phase 8: Management Ecosystem.** База данных пользователей, Web-интерфейс администратора (генерация ключей, статистика трафика, QR-коды для мобильных).

## 3. Current Task
**Phase 7c (Android Client):** Портирование core-ядра на Android через gomobile bind, интеграция с VpnService, нативный UI на Kotlin.
