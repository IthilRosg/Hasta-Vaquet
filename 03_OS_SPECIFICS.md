# OS Specifics & Routing

- **Windows:**
    - Использовать `netsh` для установки MTU (1300) и DNS (1.1.1.1).
    - При включении 0.0.0.0/0 ОБЯЗАТЕЛЬНО добавить маршрут до IP сервера (берётся из `-server` флага или `config.json`) через основной шлюз, чтобы избежать рекурсии.
- **Linux:**
    - Включить IP Forwarding: `sysctl -w net.ipv4.ip_forward=1`.
    - NAT: `iptables -t nat -A POSTROUTING -s 10.0.0.0/24 -o ens3 -j MASQUERADE`.
- **Android (Phase 8 — ✅ РАБОТАЕТ):
    - VpnService через gomobile bind.
    - TUN fd → os.NewFile(uintptr(fd), "tun") → os.File.Read/Write.
    - UDP: net.Dialer.Control → Protector.protect(fd) → VpnService.protect().
    - IPv6 Blackhole: builder.addRoute("::", 0).
    - Smart Bypass: builder.addDisallowedApplication(pkg).
    - Правило рекурсии: protect() UDP до сервера ОБЯЗАТЕЛЬНО.
