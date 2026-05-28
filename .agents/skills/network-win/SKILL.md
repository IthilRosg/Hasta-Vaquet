---
name: network-win
description: |
  Windows network management: adapters, VPN, custom protocols, traffic obfuscation,
  DPI bypass, ТСПУ circumvention, MASQUE/ECH/OHTTP, Mesh VPNs, TLS fingerprint
  rotation, WebRTC masking, goodbyedpi, sing-box TUN, Gost/Brook, and full anti-DPI
  stack recommendations for RF/CN censorship environments.
disable-model-invocation: false
---

# Windows Network / VPN / Virtual Adapter Skill

Comprehensive guide for AI agents managing Windows networking: adapters, VPN,
custom protocols, traffic obfuscation, DPI (Deep Packet Inspection) bypass,
and RF-specific censorship circumvention (ТСПУ).

Covers: diagnostics, adapters, routing, DNS, firewalls, built-in VPN,
WireGuard/OpenVPN, Shadowsocks/Trojan/V2Ray/Xray (VLESS+XTLS+Reality),
QUIC-based protocols (Hysteria/TUIC/Juicity), modern obfuscation,
CDN-masking, multi-hop chains, practical bypass techniques for
Russian/Chinese DPI systems, MASQUE/ECH/OHTTP standards,
Mesh VPNs (Tailscale/NetBird/ZeroTier/Nebula), TLS fingerprint rotation,
WebRTC masking, Domain Fronting revival, Sing-box TUN, Gost/Brook,
ТСПУ next-gen detection, and full 2026 anti-DPI stack recommendations.

---

## 1. Network Adapters

| Action | Command |
|---|---|
| List adapters | `Get-NetAdapter \| ft Name, InterfaceDescription, Status, LinkSpeed` |
| IPv4/IPv6 status | `Get-NetIPAddress -AddressFamily IPv4 \| ft IPAddress, InterfaceIndex, PrefixLength` |
| Enable/Disable | `Enable-NetAdapter -Name "Name"` / `Disable-NetAdapter -Name "Name"` |
| DHCP status | `Get-NetIPInterface \| ft InterfaceAlias, Dhcp` |
| Release/Renew DHCP | `ipconfig /release && ipconfig /renew` |

Fallback for Windows 7/8: `netsh interface show interface`.

---

## 2. DNS

| Action | Command |
|---|---|
| Show DNS servers | `Get-DnsClientServerAddress \| ft InterfaceAlias, ServerAddresses` |
| Reset to auto | `Set-DnsClientServerAddress -InterfaceAlias "..." -ResetServerAddresses` |
| Set custom DNS | `Set-DnsClientServerAddress -InterfaceAlias "..." -ServerAddresses ("1.1.1.1","1.0.0.1")` |
| Flush cache | `ipconfig /flushdns` |
| Register in DNS | `ipconfig /registerdns` |
| Test resolution | `Resolve-DnsName google.com` or `nslookup google.com` |

---

## 3. Routing

| Action | Command |
|---|---|
| Show table | `route print` or `Get-NetRoute \| ft DestinationPrefix, NextHop, RouteMetric, ifIndex` |
| Add route | `route add <network> mask <mask> <gateway> metric <metric>` |
| Add persistent | `route -p add <network> mask <mask> <gateway>` |
| Delete route | `route delete <network>` |
| Default gateway | `Get-NetRoute -DestinationPrefix "0.0.0.0/0"` |

---

## 4. Built-in Windows VPN (IKEv2 / SSTP / L2TP / PPTP)

| Action | Command |
|---|---|
| List connections | `Get-VpnConnection \| ft Name, ServerAddress, TunnelType, EncryptionLevel` |
| Connect | `rasdial "Connection Name" [user pass]` |
| Disconnect | `rasdial "Connection Name" /disconnect` |
| Status | `rasdial` (no args — shows active) |
| Create | `Add-VpnConnection -Name "..." -ServerAddress "..." -TunnelType Ikev2` |
| Delete | `Remove-VpnConnection -Name "..." -Force` |

---

## 5. NAT / Port Forwarding

```
netsh routing ip nat install
netsh routing ip nat add interface "Ethernet" mode=full
netsh routing ip nat add interface "Internal" mode=private
```

Port proxy:
```
netsh interface portproxy add v4tov4 listenport=8080 connectport=80 connectaddress=192.168.1.100
netsh interface portproxy show all
netsh interface portproxy delete v4tov4 listenport=8080
```

Active connections: `netstat -ano` or `Get-NetTCPConnection`.

---

## 6. Firewall

| Action | Command |
|---|---|
| Status | `netsh advfirewall show allprofiles` |
| Enable/Disable | `netsh advfirewall set allprofiles state on` / `off` |
| Open port | `netsh advfirewall firewall add rule name="..." dir=in action=allow protocol=TCP localport=<port>` |
| PowerShell | `Get-NetFirewallProfile`, `New-NetFirewallRule` |

---

## 7. Diagnostics

| Task | Command |
|---|---|
| Basic connectivity | `ping <host>` |
| Trace route | `tracert <host>` |
| Latency + loss | `pathping <host>` |
| TCP port check | `Test-NetConnection <host> -Port <port>` |
| Active connections | `netstat -ano` |
| Reset TCP stack | `netsh int ip reset; netsh winsock reset` |

---

## 8. System Proxy

| Action | Command |
|---|---|
| Show proxy | `Get-ItemProperty "HKCU:\Software\Microsoft\Windows\CurrentVersion\Internet Settings" \| fl Proxy*` |
| Enable | `Set-ItemProperty "HKCU:..." ProxyEnable -Value 1` |
| Set server | `Set-ItemProperty "HKCU:..." ProxyServer -Value "127.0.0.1:8080"` |
| Disable | `Set-ItemProperty "HKCU:..." ProxyEnable -Value 0` |

---

## 9. Tunnels & Custom TUN/TAP

### 9.1 TUN/TAP Virtual Interfaces

| Component | Description |
|---|---|
| **WinTun** (WireGuard) | Generic TUN driver. Used by WireGuard, sing-box, Clash Meta |
| **OpenVPN TAP** | L2 bridge. Installed with OpenVPN. `Get-NetAdapter -Name "TAP*"` |
| **wintun.dll** | Core TUN library by WireGuard — `C:\Windows\System32\wintun.dll` |

### 9.2 SSH Tunnels

| Type | Command |
|---|---|
| Local (-L) | `ssh -L 8080:internal.site:80 user@gateway` |
| Remote (-R) | `ssh -R 8080:localhost:80 user@gateway` |
| Dynamic (-D, SOCKS5) | `ssh -D 1080 user@gateway` |

Persistent SOCKS5:
```
ssh -o ExitOnForwardFailure=yes -o ServerAliveInterval=60 -D 1080 -N user@gateway
```

### 9.3 Raw UDP Tunnels

**socat** (from Cygwin/MSYS2/WSL):
```
socat UDP-LISTEN:51820,fork UDP:10.0.0.1:51820
```

**ncat** (Nmap):
```
ncat --sh-exec "ncat 10.0.0.1 51820" -l -p 51820 -u
```

### 9.4 udp2raw (Fake-TCP UDP Tunnel)

Used when UDP is blocked but TCP works. Encapsulates UDP in fake TCP with 3-way handshake imitation.

- Client: `udp2raw -c -l 127.0.0.1:6666 -r server.com:1234 --raw-mode faketcp -k pass`
- Server: `udp2raw -s -l 0.0.0.0:1234 -r 127.0.0.1:51820 --raw-mode faketcp -k pass`

### 9.5 Custom TUN Tunnel (Go example)

```go
// github.com/songgao/water — TUN library
go get github.com/songgao/water
// Create TUN, read/write packets, encrypt with custom protocol,
// send via UDP/TCP/WebSocket to server.
```

---

## 10. Encrypted Proxy Protocols

### 10.1 Shadowsocks

Lightweight encrypted SOCKS5 proxy with AEAD ciphers.

| Param | Value |
|---|---|
| Port | 8388 (default) |
| Ciphers (2025) | `2022-blake3-aes-256-gcm`, `aes-256-gcm`, `chacha20-ietf-poly1305` |
| Windows Clients | **Shadowsocks-Windows**, **v2rayN**, **Clash Verge**, **Hiddify** |
| Config | `gui-config.json` or URI `ss://method:pass@host:port` |

**Status check:**
```
Get-Process | Where-Object { $_.ProcessName -match "shadowsocks|sslocal" }
netstat -ano | findstr ":1080"
```

### 10.2 Trojan

Mimics HTTPS traffic to a legitimate nginx/Apache server. Internally SOCKS5.

| Component | Purpose |
|---|---|
| `trojan` | Server + client in one binary |
| `trojan-go` | Fork with WebSocket, CDN, mux support |
| Port | 443 |
| Encryption | TLS (real browser imitation) |

Client `config.json`:
```json
{
  "run_type": "client",
  "local_addr": "127.0.0.1",
  "local_port": 1080,
  "remote_addr": "your.domain.com",
  "remote_port": 443,
  "password": ["pass"],
  "ssl": { "sni": "your.domain.com" }
}
```

### 10.3 NaiveProxy

Uses Chrome's native HTTP/2 stack (`--proxy-server` flag). Maximum plausibility — traffic is indistinguishable from a real Chrome browser.

| Action | Description |
|---|---|
| `naive` | Forward proxy binary |
| Install | `cargo install naive` or prebuilt binary |
| Config | Environment variables or YAML |

---

## 11. V2Ray / Xray Ecosystem (VMess, VLESS, Reality)

Xray is the modern fork of V2Ray with advanced obfuscation.

### 11.1 Protocols

| Protocol | Transport | Obfuscation |
|---|---|---|
| **VMess** | TCP / WebSocket / gRPC / QUIC | HTTP(S) via WS/CDN |
| **VLESS** | TCP / XTLS / Vision | XTLS — direct TLS passthrough for speed |
| **Reality** | TLS (no cert needed) | Fakes TLS to a real site, e.g. `api.github.com` |
| **Trojan** (in Xray) | TLS | Built-in, same config format |

### 11.2 Transports

| Transport | Masking |
|---|---|
| **WebSocket + TLS** | WS under `/api/ws` — hard to distinguish from legit WS |
| **gRPC** | HTTP/2 multiplexed |
| **QUIC** | HTTP/3-like UDP |
| **HTTP** | Plain HTTP request (GET/POST) |
| **XTLS Vision** | Real TLS handshake, uTLS fingerprint — browser-like |
| **Reality** | TLS to real site (no cert on server), uTLS |
| **XHTTP** (new) | Xray-specific HTTP transport, minimal overhead |

### 11.3 Reality (Xray's flagship bypass)

```
{
  "outbounds": [{
    "protocol": "vless",
    "settings": {
      "vnext": [{
        "address": "your-server.com",
        "port": 443,
        "users": [{"id": "...", "encryption": "none", "flow": "xtls-rprx-vision"}]
      }]
    },
    "streamSettings": {
      "network": "tcp",
      "security": "reality",
      "realitySettings": {
        "show": false,
        "fingerprint": "chrome",
        "serverName": "www.microsoft.com",
        "publicKey": "...",
        "shortId": "...",
        "spiderX": "/"
      }
    }
  }]
}
```

**Key**: Reality uses a real website (e.g. `www.microsoft.com`) as TLS target.
No certificate needed on your server. TLS handshake is indistinguishable
from a browser connecting to Microsoft.

### 11.4 Tools

- **v2rayN** — Windows GUI for V2Ray/Xray/Trojan/SS
- **Xray-core** — CLI: `xray run -c config.json`
- **sing-box** — universal client/server for all protocols

**Status:**
```
Get-Process | Where-Object { $_.ProcessName -match "xray|v2ray|sing-box" }
Get-NetTCPConnection -LocalPort 443 | ft LocalAddress, RemoteAddress, State
```

---

## 12. QUIC-Based Protocols

### 12.1 Hysteria 2

Uses QUIC (HTTP/3). Excels in high-latency / unstable connections.
Configurable to fake real QUIC traffic.

```
hy2 -c client.yaml
```

### 12.2 TUIC v5

Minimalist QUIC-based proxy. Fast, low overhead.

```
tuic-client -c config.json
```

### 12.3 Juicity

Newer QUIC proxy. Similar to TUIC but with improved congestion control.

```
juicity-client run -c config.json
```

### 12.4 QUIC in Xray

```json
"streamSettings": {
  "network": "quic",
  "quicSettings": {
    "security": "aes-128-gcm",
    "key": "pass",
    "header": { "type": "dtls" }
  }
}
```

---

## 13. Obfuscation & DPI Bypass

### 13.1 OpenVPN Scramble

| Flag | Description |
|---|---|
| `--scramble obfuscate` | XOR mask all packets |
| `--scramble xormask <key>` | XOR with custom key |
| `--scramble reverse` | Reverse packet bytes |
| `--tls-crypt` | Encrypt TLS channel |
| `--tls-crypt-v2` | Per-client keys, harder to DPI |

### 13.2 obfs4 (Tor)

Tor obfuscation — traffic looks like random noise.

- `obfs4proxy` — transport plugin
- Bridge: `Bridge obfs4 <ip>:<port> <cert> iat-mode=0`

### 13.3 AmneziaWG

WireGuard fork with built-in obfuscation: random padding, packet size
normalization, protocol masking. Blocks WG fingerprint detection.

- `<AmneziaVPN>` — Windows client with WG obfuscation
- Native `wg-quick` with `AmneziaWG` binary if installed separately

### 13.4 Fragment & MTU Tricks

| Method | Implementation |
|---|---|
| MSS clamping | `netsh int tcp set global mss=1300` |
| PMTU Discovery | `netsh int tcp set global pmtud=1` |
| Xray Fragment | `"fragment": { "packets": "tlshello", "length": "50-100" }` |
| SplitHTTP (sing-box) | Split HTTP requests to bypass SNI-based DPI |

### 13.5 CDN Masking (WebSocket + CDN)

```
[Client] → WSS → [Cloudflare CDN] → WSS → [Your Server:443]
```

- Your domain points to Cloudflare CDN
- CDN proxies WebSocket traffic to origin server
- Real server IP is hidden behind CDN
- Traffic looks like normal HTTPS to CDN's IP

Works with: XRay/V2Ray WS transport, Trojan-go, sing-box WS outbound.

### 13.6 TLS Fingerprint Randomization (uTLS / XTLS Vision)

Modern DPI systems (ТСПУ) fingerprint TLS handshakes to identify
proxies. Mitigation:

- **uTLS** — library that emulates Chrome/Firefox/Safari TLS fingerprints
- **XTLS Vision** — Xray flow using uTLS, creates browser-identical TLS
- **Reality** — uses real TLS to a real website (Microsoft, Github, etc.)
- **sing-box uTLS** — built-in fingerpint: `"fingerprint": "chrome"` or `"random"`

```json
"tls": {
  "enabled": true,
  "fingerprint": "chrome",
  "serverName": "www.microsoft.com"
}
```

### 13.7 MPTCP (Multipath TCP)

Windows 11 supports MPTCP. Splits traffic across multiple network paths,
complicating DPI analysis.

```
netsh int tcp set global mptcp=enabled
```

---

## 14. DPI Detection & Analysis (ТСПУ)

Knowledge of how modern DPI/censorship systems work helps choose the right bypass.

### 14.1 Russian ТСПУ (Technical Counter-Measures System)

**What it detects:**
- SNI (Server Name Indication) — matches against banned domain lists
- TLS certificate — inspects ServerHello for unrecognized certs
- Active probing — DPI sends fake RST to test if port is really open
- Protocol fingerprint — OpenVPN, WireGuard, Shadowsocks handshake patterns
- TLS fingerprint (JA3/JA3S) — non-browser TLS implementations are flagged
- Time-based analysis — proxy round-trip timing vs direct connection timing
- Packet size distribution — proxy protocols have characteristic patterns
- DPI reset — sends `RST` packets to kill connections

**Что Роскомнадзор блокирует через ТСПУ (as of 2025–2026):**
- VPN/прокси протоколы (OpenVPN, WireGuard, IPSec, L2TP, PPTP) — по fingerprint
- Shadowsocks, V2Ray, Xray — по handshake signature
- DoH/DoT — известные серверы (Cloudflare, Google, Quad9) заблокированы
- Telegram (частично) — периодически замедляется
- YouTube — сильно замедлен (throttled to ~128-256 Kbps)
- Discord — замедлен, иногда полностью блокируется
- Twitch — замедлен
- LinkedIn — заблокирован
- Instagram/Facebook — замедлены, частично недоступны
- Steam Community — замедлен
- Pinterest / Snapchat — ограничены

### 14.2 Detection Techniques

| Technique | What it looks for | Bypass |
|---|---|---|
| **SNI filtering** | Domain in ClientHello | Use CDN (Cloudflare) + WS, or Reality (no SNI to your server) |
| **JA3/JA3S fingerprint** | TLS cipher suites & extensions | uTLS (Chrome/Firefox/Safari emulation) |
| **Active probing** | DPI connects to server to verify proxy | Block all ports except 443, authenticate handshake |
| **Packet timing** | RTT distribution analysis | Add random delays, shape traffic (Hysteria) |
| **Packet size** | Characteristic packet sizes | Fragment/pad packets (AmneziaWG, Xray Fragment) |
| **Protocol handshake** | Known patterns (WG, SS, OpenVPN) | Tunnel protocol under another (WG→udp2raw, SS→WSS) |
| **HTTP host** | Host header in plain HTTP | Use TLS everywhere |

### 14.3 Testing for DPI

```
# Check if DPI is sending RST packets
Test-NetConnection blocked.site.com -Port 443 -WarningAction SilentlyContinue
# If connection succeeds but immediate RST — DPI active

# Verify SNI vs IP blocking
Resolve-DnsName blocked.site.com
# Ping the IP directly — if IP works, blocking is SNI-based

# Check if DoH is blocked
curl.exe -k https://1.1.1.1/dns-query
# If fails, DoH is blocked by DPI

# Check TLS fingerprint detection
# Use https://github.com/sergey-se/Alice (Russian tool) 
```

### 14.4 Common Blocking Patterns (RF)

| Service | Blocking Method | Status (2025–2026) |
|---|---|---|
| YouTube | Throttling + DPI | Heavily throttled |
| Discord | DPI + IP block | Degraded, unstable |
| Twitch | SNI + IP block | Heavily restricted |
| LinkedIn | SNI block | Fully blocked |
| Facebook/Instagram | SNI + throttling | Heavily degraded |
| ProtonVPN / NordVPN | IP block | Mostly blocked |
| Outline VPN | Protocol fingerprint | Detected and blocked |
| Standard WireGuard | Handshake fingerprint | Detected if no obfuscation |

---

## 15. Practical Bypass Strategies

### 15.1 Recommended Stack for РФ (2025–2026)

**Option A — Xray Reality** (best for users without a CDN domain):
```
[Client: Xray/V2rayN] → Reality (TLS to microsoft.com) → [Server: Xray]
```
Why: No custom TLS certificate needed. Traffic is indistinguishable from
browsing Microsoft/GitHub.

**Option B — VLESS + XTLS Vision + uTLS**:
```
[Client] → VLESS over TLS (Chrome fingerprint) → [Server]
```

**Option C — WebSocket + CDN**:
```
[Client] → WSS to CDN → CDN forward → [Server with WS]
```
Why: DPI sees Cloudflare IP + valid TLS. Server IP hidden.

**Option D — WireGuard over AmneziaWG**:
```
[Client: AmneziaWG client] → obfuscated WG → [Server: AmneziaWG]
```
Why: WG is fast but detected; AmneziaWG adds packet padding + masking.

**Option E — ShadowSocks over WebSocket**:
```
[Client] → SS → WSS tunnel → [Server]
```

**Option F — Multi-hop chain**:
```
[App] → Clash Meta/sing-box → [VLESS + Reality] → [Shadowsocks] → [Hysteria 2] → [Internet]
```

### 15.2 GoodbyeDPI / Zapret (Russian Tools)

**GoodbyeDPI** (by ValdikSS) — Windows tool that bypasses DPI by manipulating
TCP packets at driver level. Useful when you DON'T want a full VPN/proxy but
just need to access blocked sites directly.

```
# Download from https://github.com/ValdikSS/GoodbyeDPI
goodbyedpi.exe -5 --blacklist domains.txt --dns-addr 1.1.1.1 --dns-port 53
```

**Zapret** (by bol-vap) — Linux/MikroTik tool, also available via WSL.
```
zapret.sh start
```

Both work by:
- Fragmenting the first TLS packet (before DPI sees SNI)
- Modifying TCP MSS
- Adding dummy TCP options
- Changing TTL on suspicious packets

### 15.3 Obfs4 via Tor Bridges

When even Xray is blocked, fall back to Tor with obfs4:
```
tor --use-bridge obfs4 <ip>:<port> <cert> iat-mode=0
# Then use Tor SOCKS5 at 127.0.0.1:9050
```

### 15.4 DNS Bypass

Since known DoH servers (1.1.1.1, 8.8.8.8) are often blocked:
- **DNSCrypt-Proxy** — supports DoH/DoT/DoQ over port 443
- **dnscrypt-proxy** with `server_names = ['cloudflare']` but use `force_port = 443`
- Self-hosted DoH behind CDN (e.g., `your-domain.com/dns-query` via Cloudflare Worker)
- **DNS over Oblivious HTTP (DoOH)** — experimental, hides destination domain

```
# dnscrypt-proxy config
listen_addresses = ['127.0.0.1:53']
doh_servers = true
force_port = 443
```

### 15.5 Fragmentation as a Universal Bypass

When NAT/DPI is analyzing packets, fragmentation can break it:

```
# Windows — reduce MSS
netsh int tcp set global mss=1200

# GoodbyeDPI — fragment TLS hello
goodbyedpi.exe -1 -2 -3 -4 -5

# Xray Fragment:
"fragment": {
  "packets": [
    {"length": "50-100", "interval": "10-20"}
  ]
}
```

---

## 16. Universal Proxy Clients

### 16.1 Clash Verge Rev

Supports: Shadowsocks, VMess, VLESS, Trojan, Hysteria, TUIC, WireGuard.

| File | Description |
|---|---|
| `config.yaml` | Main config with proxies, rules, DNS |
| `Cache.db` | DNS cache — clear with `del Cache.db` |
| Port | 7890 (HTTP/SOCKS5), 7891 (HTTP) |

Check:
```
Get-Process clash*
Invoke-WebRequest -Uri http://127.0.0.1:9090 -Proxy http://127.0.0.1:7890
```

### 16.2 sing-box

Universal client/server (Xray + SS + Trojan + WG + Hysteria + TUIC).

| Action | Command |
|---|---|
| Run | `sing-box run -c config.json` |
| Validate | `sing-box check -c config.json` |
| Format | `sing-box format -c config.json` |

Typical config:
```json
{
  "inbounds": [{"type": "socks", "listen": "127.0.0.1", "listen_port": 1080}],
  "outbounds": [{"type": "vless", ...}]
}
```

### 16.3 Hiddify Next

Simple multi-platform client. Supports all major protocols.
One-click config import via subscription links.

- Download: [github.com/hiddify/hiddify-next](https://github.com/hiddify/hiddify-next)
- Uses sing-box engine under the hood

---

## 17. Multi-hop Chains

```yaml
[App] → [Clash/sing-box] → [VLESS Reality] → [Shadowsocks] → [Hysteria] → [Internet]
```

| Combination | Tool |
|---|---|
| SOCKS5 → Shadowsocks | Clash Meta / sing-box (relay) |
| SS → WireGuard | Route all traffic except WG server through SS |
| Tor → VPN | VPN first, then Tor. Hides Tor from ISP |
| VPN → Tor | Tor first, then VPN. Exit via VPN IP |

sing-box multi-hop:
```json
{
  "outbounds": [
    {"tag": "ss", "type": "shadowsocks", "server": "ss-server", ...},
    {"tag": "hysteria", "type": "hysteria2", "server": "hy-server", ...},
    {
      "tag": "chain",
      "type": "selector",
      "outbounds": ["ss"],
      "detour": "hysteria"
    }
  ]
}
```

---

## 18. Encrypted DNS

| Tech | Type | Windows Setup |
|---|---|---|
| **DoH** | HTTPS | `Add-DnsClientDohServerAddress -ServerAddress "1.1.1.1" -DohTemplate "https://cloudflare-dns.com/dns-query"` |
| **DoT** | TLS | Windows 11: Settings → Network → DNS → Encrypted |
| **DoQ** | QUIC | Via dnscrypt-proxy |
| **DNSCrypt** | Encrypted | `dnscrypt-proxy -config dnscrypt.toml` (local :53 → DoH/DoT) |

**Check:**
```
Get-DnsClientDohServerAddress | ft ServerAddress, DohTemplate, AllowFallbackToUdp
Get-NetAdapter | Get-DnsClientServerAddress
```

**Self-hosted DoH behind Cloudflare Worker:**
```js
// Cloudflare Worker for DNS-over-HTTPS
addEventListener('fetch', event => {
  event.respondWith(handleRequest(event.request))
})
async function handleRequest(request) {
  const url = new URL(request.url)
  const dnsQuery = url.searchParams.get('dns')
  if (!dnsQuery) return new Response('Missing dns param', {status: 400})
  const dnsResponse = await fetch('https://1.1.1.1/dns-query?dns=' + dnsQuery, {
    headers: { 'Accept': 'application/dns-message' }
  })
  return new Response(await dnsResponse.arrayBuffer(), {
    headers: { 'Content-Type': 'application/dns-message' }
  })
}
```

---

## 19. Tools & References

| Tool | Purpose | Link |
|---|---|---|
| **sing-box** | Universal client/server | [github.com/SagerNet/sing-box](https://github.com/SagerNet/sing-box) |
| **v2rayN** | Windows GUI for Xray/V2Ray | [github.com/2dust/v2rayN](https://github.com/2dust/v2rayN) |
| **Clash Verge Rev** | GUI for Clash Meta / sing-box | [github.com/clash-verge-rev/clash-verge-rev](https://github.com/clash-verge-rev/clash-verge-rev) |
| **Hiddify Next** | Multi-platform client | [github.com/hiddify/hiddify-next](https://github.com/hiddify/hiddify-next) |
| **Hysteria 2** | QUIC proxy for bad networks | [github.com/apernet/hysteria](https://github.com/apernet/hysteria) |
| **Xray-core** | Advanced proxy platform | [github.com/XTLS/Xray-core](https://github.com/XTLS/Xray-core) |
| **AmneziaWG** | Obfuscated WireGuard | [github.com/amnezia-vpn/amneziawg-windows-client](https://github.com/amnezia-vpn/amneziawg-windows-client) |
| **udp2raw** | UDP over fake TCP | [github.com/wangyu-/udp2raw](https://github.com/wangyu-/udp2raw) |
| **GoodbyeDPI** | DPI bypass (Windows) | [github.com/ValdikSS/GoodbyeDPI](https://github.com/ValdikSS/GoodbyeDPI) |
| **Zapret** | DPI bypass (Linux/MikroTik) | [github.com/bol-vap/zapret](https://github.com/bol-vap/zapret) |
| **DNSCrypt-Proxy** | Encrypted DNS proxy | [github.com/DNSCrypt/dnscrypt-proxy](https://github.com/DNSCrypt/dnscrypt-proxy) |
| **Alice** | Russian DPI detection tool | [github.com/sergey-se/Alice](https://github.com/sergey-se/Alice) |
| **uTLS** | TLS fingerprint library | [github.com/refraction-networking/utls](https://github.com/refraction-networking/utls) |
| **Juicity** | QUIC proxy (new) | [github.com/juicity/juicity](https://github.com/juicity/juicity) |

---

## 20. Detection & Audit Commands

### 20.1 Am I behind DPI?

```powershell
# Check if DPI RST is active
Test-NetConnection -ComputerName microsoft.com -Port 443 -TraceRoute

# If connection is RST but server is alive — DPI!
curl.exe -v https://microsoft.com 2>&1 | findstr "RST"

# Check SNI vs IP
Test-NetConnection -ComputerName blocked.site.com -Port 443
Resolve-DnsName blocked.site.com
# Ping IP directly: if IP works but hostname doesn't → SNI blocking
```

### 20.2 Checking TLS Fingerprint

```powershell
# Use Alice tool
alice.exe -target google.com
# Output: JA3, JA3S, TLS version, cipher suites
```

### 20.3 Verify bypass is working

```powershell
# Check leaked IP (should be proxy/VPN IP, not your ISP)
curl.exe --proxy socks5://127.0.0.1:1080 https://ipinfo.io/json

# DNS leak test
curl.exe --proxy socks5://127.0.0.1:1080 https://ipleak.net
# Also: nslookup should go through encrypted DNS, not ISP DNS

# WebRTC leak test (in browser)
# https://browserleaks.com/webrtc
```

### 20.4 Process & Port Audit

```powershell
# All listening ports
netstat -ano | findstr LISTEN

# Which process uses port 1080 (SOCKS5)?
Get-Process -Id (Get-NetTCPConnection -LocalPort 1080).OwningProcess

# All proxy-related processes
Get-Process | Where-Object { $_.ProcessName -match "xray|v2ray|clash|trojan|shadowsocks|hysteria|sing-box|tuic|juicity|wg|openvpn" } | ft Name, Id
```

---

## ⚠️ Important Notes

- Most tools in sections 9–19 are NOT built into Windows. Install via
  Winget/Choco/Scoop or download from GitHub.
- `netsh int tcp set global mss=...` may reduce performance but helps
  with MTU issues and some DPI systems.
- WinTun is required for most modern proxies (install WireGuard or
  drop `wintun.dll` alongside the app).
- SDN masking requires your own domain + CDN (Cloudflare Free tier works).
- Xray Reality requires a server with a valid domain pointing to a real
  website; your Xray runs on the same server and shares port 443.
- ТСПУ systems evolve rapidly — always test with the latest version of
  your chosen tool.
- Using these techniques may violate local laws. Know your jurisdiction.

---

## Practical Scenario Examples

**Scenario 1: Everything is blocked (RF, 2025)**
```
1. Set up Xray Reality server with www.microsoft.com as target
2. Client: v2rayN or sing-box with Reality outbound
3. DNS: dnscrypt-proxy → custom DoH endpoint
4. Verify: ipinfo.io shows your server IP, not your home IP
```

**Scenario 2: UDP is completely blocked**
```
1. Set up udp2raw on server: udp2raw -s -l :1234 -r 127.0.0.1:51820 --raw-mode faketcp -k key
2. Set up udp2raw on client: udp2raw -c -l 127.0.0.1:6666 -r your-server.com:1234 --raw-mode faketcp -k key
3. WireGuard connects to 127.0.0.1:6666
4. Alternatively: Xray with TCP transport instead of QUIC
```

**Scenario 3: Need to access blocked site without full VPN**
```
1. Run GoodbyeDPI: goodbyedpi.exe -5 --dns-addr 1.1.1.1 --dns-port 53
2. Or set up sing-box as local proxy with Reality outbound
3. Configure Firefox/Chrome to use system proxy (127.0.0.1:7890 in Clash)
```

**Scenario 4: CDN-based bypass**
```
1. Buy a domain, point to Cloudflare (orange cloud = proxied)
2. Install xray on your server, configure:
   - Network: WebSocket
   - Path: /secret123
   - Host: your.domain.com
3. On client: VLESS/WS/TLS with your.domain.com
4. Traffic hits Cloudflare IP → Cloudflare WS forwards to your server
```

**Scenario 5: Chain for maximum anti-DPI**
```
sing-box chain: [VLESS Reality (Chrome fg)] → [Hysteria 2 (QUIC)]
- First hop: looks like Chrome to microsoft.com
- Second hop: QUIC transport, random packet sizes
- DNS: DoH via Cloudflare Worker on your own domain
```

---

## 21. IETF Standards & Emerging Protocols (2025–2026)

### 21.1 MASQUE (Multiplexed Application Substrate over QUIC Encryption)

MASQUE is an IETF standard (RFC 9484, 9640, 9641) that extends HTTP/3
(CONNECT) to proxy arbitrary IP packets over QUIC. Unlike traditional
VPNs, MASQUE is:
- Standardized — no custom protocol fingerprint
- Runs over QUIC (HTTP/3) — indistinguishable from browser QUIC
- Multiplexed — multiple streams over one QUIC connection
- Approved in Chrome (via `--enable-features=UseDnsHttpsSvcb,EnableMASQUE`)

**Windows implementation:**
- `sing-box` supports MASQUE outbound (2025+):
```json
{
  "type": "masque",
  "server": "your-server.com",
  "server_port": 443,
  "tls": { "enabled": true, "fingerprint": "chrome" }
}
```
- Tailscale uses MASQUE for its DERP relay servers
- Chrome has built-in MASQUE CONNECT-IP support (flags)

**Why it matters:** MASQUE traffic looks exactly like HTTP/3 QUIC to a
CDN (Cloudflare supports QUIC). No DPI system can distinguish MASQUE
from normal QUIC traffic without deep content inspection.

### 21.2 CONNECT-UDP (RFC 9640)

Extension of HTTP CONNECT for UDP proxying. Allows DNS-over-HTTPS,
QUIC, and WireGuard traffic over HTTP/3 proxies.

### 21.3 CONNECT-IP (RFC 9484)

Extension that proxies entire IP packets (TUN mode) over HTTP/3.
Effectively a VPN inside QUIC, fully standardized.

### 21.4 ECH (Encrypted Client Hello, RFC 8871, 9460)

Encrypts the entire TLS ClientHello — including SNI — so DPI cannot
see which website you're connecting to.

| Status | Details |
|---|---|
| Supported by | Cloudflare, Fastly, some CDNs |
| Browser support | Chrome (flag), Firefox (flag) |
| Windows support | Not natively; requires DoH + HTTPS DNS records (SVCB/HTTPS) |

**To enable ECH on Windows (via browser):**
```
chrome://flags/#encrypted-client-hello
# Enable and restart Chrome
```

**To use ECH + DoH for SNI hiding:**
```
# DNS must return HTTPS/SVCB records with ECH config
Resolve-DnsName cloudflare.com -Type HTTPS
# If ECH keys present, browser auto-enables ECH
```

### 21.5 OHTTP (Oblivious HTTP, RFC 9458)

Separates WHO you are (IP) from WHAT you're requesting. Uses a relay
that strips origin IP before forwarding to the target server.

- Two-hop architecture: [Client] → [OHTTP Relay] → [Target Server]
- Relay sees client IP but NOT the request content
- Target server sees request but NOT the client IP
- Implemented in: Cloudflare Privacy Proxy, Apple iCloud Private Relay

**Oblivious DNS-over-HTTPS (ODoH):**
```
# Uses two independent servers:
# 1. Proxy (sees IP, not DNS query)
# 2. Target (sees DNS query, not IP)
# This breaks correlation even if one server is compromised
```

### 21.6 DNS SVCB/HTTPS Records (RFC 9460)

New DNS record type that carries ECH keys, ALPN, port numbers.
Enables ECH without manual configuration.

```
# Query HTTPS record
Resolve-DnsName example.com -Type HTTPS
```

---

## 22. Mesh VPNs (Zero-Trust Networking)

Mesh VPNs replace traditional hub-and-spoke VPNs. Every peer connects
directly (peer-to-peer) via NAT traversal (STUN/TURN/ICE). Traffic is
WireGuard-based or custom.

### 22.1 Tailscale / Headscale

| Feature | Tailscale | Headscale (OSS) |
|---|---|---|
| Protocol | WireGuard + DERP (MASQUE) | WireGuard + DERP |
| Auth | SSO (Google, GitHub, Microsoft) | OIDC, any |
| Control plane | Tailscale SaaS | Self-hosted |
| Windows client | Tailscale GUI | `headscale` CLI
| Bypass capability | DERP relays can be self-hosted | Same |

**Windows CLI:**
```
tailscale status          # show connected peers
tailscale up --login-server https://headscale.example.com
tailscale set --accept-routes=true  # accept subnet routes
```

**Why for bypass:** Tailscale traffic = WireGuard. With DERP relays
running on your own domain behind CDN, it becomes WS+CDN + WG.

### 22.2 NetBird

Open-source mesh VPN. Uses WireGuard + NAT traversal.

```
netbird up --setup-key <key>
netbird status
```

### 22.3 ZeroTier

L2 mesh VPN. Creates virtual Ethernet networks.

```
# Install ZeroTier One (Windows service)
zerotier-cli join <network-id>
zerotier-cli listnetworks
```

### 22.4 Nebula (Slack)

Scalable overlay networking. Uses its own protocol (not WireGuard),
with built-in certificate auth and lighthouse nodes for NAT traversal.

```
nebula -config config.yml
# Check status
nebula -status
```

### 22.5 When Mesh VPNs Beat Traditional Proxy

| Scenario | Mesh VPN | Traditional Proxy |
|---|---|---|
| Access internal corp resources | ✅ Direct P2P | ❌ Needs port forward |
| Friend-to-friend direct connection | ✅ NAT traversal | ❌ Requires server |
| Hide traffic pattern (24/7 tunnel) | ✅ Constant WG noise | ⚠️ Proxy on-demand |
| Many devices with ACL | ✅ Built-in | ❌ Manual routing |

---

## 23. Advanced Obfuscation (2025–2026)

### 23.1 TLS Fingerprint Rotation (JA3 Cycling)

Next step beyond uTLS — change TLS fingerprint for every connection.
DPI cannot build a stable fingerprint profile.

```json
// sing-box -> advanced fingerprint
"tls": {
  "enabled": true,
  "fingerprint": "random",        // random from a pool of browsers
  "random_fingerprint": true,     // rotate per connection
  "fallback_sni": "microsoft.com"
}
```

### 23.2 WebRTC Masking

WebRTC (used by video calls, Discord, Zoom) is rarely blocked.
New tools encapsulate proxy traffic inside WebRTC data channels.

- **WebRTC VPN** — experimental: proxy inside PeerConnection
- Traffic looks like STUN/TURN/ICE — indistinguishable from real calls
- Hard to block without breaking real video conferencing

### 23.3 Fake DNS Masquerading

Encapsulates proxy traffic inside DNS queries (DNS-over-HTTPS-style).
Since DNS is essential and rarely fully blocked:

- **DNSTT** (DNS Tunnel) — mature, but slow
- **DoH-based tunnels** — proxy traffic inside DNS queries to your
  custom DoH endpoint behind CDN
- DPI sees: `GET /dns-query?dns=...` (looks like DNS)
- Actually contains: encrypted proxy payload

### 23.4 Domain Fronting Revival (2025–2026)

Domain Fronting was "patched" by CDNs in 2018, but some niche CDNs
and self-configured reverse proxies still allow it.

**How it works:**
- TLS SNI = `fronting-cdn.com` (allowed)
- HTTP Host header = `your-hidden-server.com` (actual target)
- CDN routes based on Host header, not SNI

**Current status:**
- ❌ Cloudflare blocks fronting
- ✅ Some smaller CDNs still allow it
- ✅ Azure CDN / Akamai (certain configs)
- ✅ Self-hosted reverse proxy (nginx/caddy) with custom SNI routing

**Revival path:** Using `CONNECT` method with separate SNI and Host:
```
curl --connect-to ::fronting-cdn.com:443 \
     -H "Host: your-hidden-server.com" \
     https://your-hidden-server.com/
```

### 23.5 Rotating CDN / Auto-Migration

When a CDN IP range gets blocked:

- **Clouflare Warp+** as upstream → IP rotates every few minutes
- **Multi-CDN**: bounce between Cloudflare, Fastly, Bunny CDN, Azure
- **Automatic failover**: script checks if CDN IP is reachable;
  if blocked, switch DNS to another CDN

```bash
# Simple CDN-rotate script concept
for cdn in "1.1.1.1" "8.8.8.8" "185.199.108.153"; do
  ping -n 1 $cdn >nul && echo "$cdn reachable"
done
```

### 23.6 HTTP/3 CONNECT (QUIC Proxy)

Chrome and Firefox now support HTTP/3 CONNECT proxy (proxying via QUIC).

```
# Chrome uses QUIC by default for proxies
# sing-box as CONNECT-UDP proxy:
{
  "type": "socks",
  "udp_over_tcp": false,  // prefer QUIC
  "tcp_fast_open": true
}
```

### 23.7 SSH over WebSocket

When SSH is blocked, tunnel it over WebSocket:

```
# Client: websocat (or browser) connects to WSS
websocat -v wss://your-server.com/ssh --ws-c-uri=ssh://user@localhost

# Server: ws-tcp-bridge
ws-tcp-bridge --l 0.0.0.0:443 --target localhost:22
```

### 23.8 HTTP/3 Tunneling with Sing-box

```json
{
  "type": "direct",
  "dialer": {
    "type": "quic",
    "quic": {
      "fingerprint": "chrome",
      "congestion_control": "bbr"
    }
  }
}
```

---

## 24. Next-Gen Tools & Utilities

### 24.1 Gost (GO Simple Tunnel) — Swiss Army Knife

Gost supports: HTTP/SOCKS4/SOCKS5/Shadowsocks/Relay/TUN/Redirect
with any combination of ingress→egress.

```bash
# SOCKS5 → Shadowsocks → Relay
gost -L socks://:1080 -F ss://method:pass@server:8388 -F relay://server2:1234

# WireGuard-over-TUN via gost
gost -L tun://:0.0.0.0:8421 -F socks5://proxy:1080
```

**Unique feature:** gost can chain ANY protocol with ANY other:
`[ingress] → [hop1] → [hop2] → ... → [egress]`

### 24.2 Brook

Simple cross-platform VPN/proxy. One binary, minimal config.

```
# Server
brook server -l :9999 -p password

# Client (creates TUN interface)
brook client -s server.com:9999 -p password --socks5 127.0.0.1:1080

# Brook with WebSocket
brook wsserver -l :9999 -p password
brook wsclient -s wss://server.com:9999 -p password --socks5 127.0.0.1:1080
```

### 24.3 BoringTun (Cloudflare)

Userspace WireGuard implementation in Rust. No kernel driver needed.

```
# Run as TUN service
boringtun-cli --foreground wg0
```

### 24.4 WireProxy

Runs WireGuard client as a SOCKS5 proxy. Useful when you cannot
create a TUN interface.

```
wireproxy --config wg.conf
# Now use 127.0.0.1:1080 as SOCKS5
```

### 24.5 GoQuiet

Shadowsocks obfuscation plugin that makes SS traffic look like TLS.

```
# Client config: use GoQuiet as SS plugin
sslocal -c config.json --plugin gq-client --plugin-opts config.toml
```

### 24.6 Sing-box TUN Mode (Full VPN Replacement)

Sing-box can create a TUN interface and route ALL traffic:

```json
{
  "inbounds": [{
    "type": "tun",
    "interface_name": "tun0",
    "mtu": 1500,
    "inet4_address": "10.0.0.1/30",
    "auto_route": true,
    "strict_route": false
  }]
}
```

On Windows, sing-box TUN requires WinTun, creates a virtual adapter,
and routes all traffic through proxy chains. This is a full VPN
replacement with all obfuscation options.

### 24.7 Rainbow (New, 2026)

Emerging multi-protocol obfuscation proxy. Dynamically switches
protocols mid-connection to evade pattern analysis.

```
# Conceptual (check latest releases)
rainbow client --config rainbow.yaml
# Rotates between: SS → VLESS → Hysteria → WireGuard
```

---

## 25. ТСПУ Next Generation & Counter-Strategies

### 25.1 What ТСПУ 2.0+ Might Detect (2025–2026 Trend)

| Detection Method | What It Targets | Bypass |
|---|---|---|
| **TLS Resumption fingerprint** | Session ticket reuse patterns | Disable session resumption or randomize ticket lifetime |
| **HTTP/3 (QUIC) fingerprint** | QUIC version, transport params | Use uTLS-style QUIC fingerprint (sing-box `quic.fingerprint`) |
| **ML-based classification** | Statistical traffic patterns | Random padding + protocol rotation (Rainbow-style) |
| **ECH blocking** | Known ECH endpoints | Self-hosted ECH behind CDN |
| **CDN IP range blocking** | Cloudflare/Akamai ASNs | Rotating CDN, self-hosted reverse proxy |
| **DNS timing correlation** | Correlate DNS query time with connection time | DNS pipelining, ODoH |
| **Active HTTP/3 probing** | Probe QUIC endpoints for proxy behavior | Require valid TLS + HTTP/3 response |

### 25.2 ECH Blockade & Workarounds

Some DPI systems now block traffic that uses ECH (Encrypted Client
Hello) by dropping packets with ECH extensions.

**Workaround**: Use ECH inside WebSocket over CDN:
```
[Client] → WSS to CDN (with ECH) → CDN → [Your Server]
# CDN terminates TLS with ECH, then opens new TLS to your server
# DPI sees: ECH to CDN (allowed) + unknown destination (CDN IP)
```

### 25.3 Machine Learning DPI & Countermeasures

Modern DPI uses ML to classify traffic by:
- **Packet inter-arrival time** — VPN/proxy has different timing
- **Burst patterns** — proxy protocols have characteristic bursts
- **Bidirectional ratios** — asymmetry of upload/download

**Countermeasures:**
- Hysteria 2 traffic shaping: add random delays
- AmneziaWG: normalize packet sizes to uniform distribution
- sing-box `—mode normal` vs `—mode paranoid` for timing obfuscation
- Multi-protocol rotation every N minutes

### 25.4 Hardware-Based ТСПУ

New-generation ТСПУ devices (2025+) operate at 100Gbps+ with:
- **TCAM-based pattern matching** at line rate
- **FPGA-accelerated TLS fingerprinting** (JA3 in hardware)
- **NetFlow v10 + Deep Packet Inspection** correlation

**Counter-strategies:**
- Hardware DPI is stateless — it relies on templates
- Protocol rotation defeats template matching
- Full encryption + padding + fingerprint randomization defeats
  hardware inspection

### 25.5 Testing Against Next-Gen DPI

```powershell
# Check if TLS resumption is being tracked
# Connect twice and compare session ticket:
curl.exe -v https://blocked.site.com 2>&1 | findstr "session"

# Check if QUIC is being throttled (different from TCP)
Test-NetConnection blocked.site.com -Port 443
# Compare with:
test-netconnection blocked.site.com -Port 443 -Protocol QUIC

# Multi-path test: if TCP works but QUIC doesn't → QUIC blocking
```

---

## 26. Complete Anti-DPI Stack Recommendations (2026)

### Layer 1: Transport Obfuscation
| Technique | Tool | Priority |
|---|---|---|
| TLS fingerprint randomization | sing-box `fingerprint: "chrome"` | ✅ MUST |
| TLS fingerprint rotation | sing-box `fingerprint: "random"` | ⭐ BEST |
| Encrypted SNI (ECH) | Cloudflare + Chrome flag | ✅ SHOULD |
| MTU/MSS clamping | `netsh int tcp set global mss=1300` | ⚠️ IF needed |

### Layer 2: Protocol Selection
| Protocol | Detection Risk | Recommendation |
|---|---|---|
| Xray Reality (TLS to real site) | VERY LOW | 🥇 Best standalone |
| WSS + CDN (WS behind Cloudflare) | VERY LOW | 🥇 Best with domain |
| VLESS + XTLS Vision | LOW | 🥈 Good |
| Hysteria 2 (QUIC mask) | MEDIUM | 🥈 Good for bad networks |
| AmneziaWG | MEDIUM | 🥈 Good for speed |
| Shadowsocks + WSS | MEDIUM | 🥉 Fallback |
| Trojan | HIGH (detected) | ❌ Avoid |
| Standard WireGuard | HIGH (fingerprinted) | ❌ Avoid without obfuscation |

### Layer 3: DNS Security
| Method | Status |
|---|---|
| Self-hosted DoH behind CDN | ✅ BEST — looks like HTTPS to CDN |
| DNSCrypt-proxy with force_port=443 | ✅ GOOD — hard to block |
| ODoH (Oblivious DNS) | ✅ GOOD — splits IP from query |
| Public DoH (1.1.1.1, 8.8.8.8) | ❌ Blocked in RF |
| ISP DNS | ❌ No encryption |

### Layer 4: CDN Strategy
| Strategy | When |
|---|---|
| Single CDN (Cloudflare) | Default — works for most cases |
| Multi-CDN failover | If Cloudflare IPs get blocked |
| Self-hosted reverse proxy | For maximum control |
| DERP relay (Tailscale) | For mesh VPN with CDN-like relay |

### Layer 5: Traffic Shaping
| Feature | Tool |
|---|---|
| Random packet padding | AmneziaWG, sing-box |
| Timing obfuscation | Hysteria 2 (`hop_interval`) |
| Protocol rotation | Rainbow (emerging), manual switch |
| Packet fragmentation | GoodbyeDPI, Xray Fragment |

### 2026 Recommended Stack (From Most to Least Censored)

**Heavy censorship (RF/CN):**
```
1. Self-hosted DoH via Cloudflare Worker (ENSURES DNS privacy)
2. Xray Reality → www.microsoft.com OR Cloudflare CDN + WS + VLESS
3. uTLS fingerprint: Chrome (or random rotation in sing-box)
4. ECH enabled in browser (Chrome flag)
5. Optional: GoodbyeDPI for non-browser traffic fragmentation
6. Check: ipinfo.io shows server IP, not home IP
```

**Medium censorship:**
```
1. DNSCrypt-proxy with force_port=443
2. AmneziaWG or Hysteria 2 (single hop, no CDN needed)
3. TLS fingerprint: chrome
```

**Light censorship / privacy only:**
```
1. Tailscale or NetBird mesh VPN
2. Mullvad or Proton VPN (paid)
3. Or just Warp (1.1.1.1) for basic DNS + encryption
```

---

## References & Further Reading

| Resource | Link |
|---|---|
| Xray Reality docs | [XTLS/Xray-core](https://github.com/XTLS/Xray-core) |
| Sing-box documentation | [sing-box.sagernet.org](https://sing-box.sagernet.org) |
| MASQUE IETF | [datatracker.ietf.org/wg/masque](https://datatracker.ietf.org/wg/masque/) |
| ECH RFC 8871 | [RFC 8871](https://datatracker.ietf.org/doc/rfc8871/) |
| Oblivious HTTP RFC 9458 | [RFC 9458](https://datatracker.ietf.org/doc/rfc9458/) |
| GoodbyeDPI | [ValdikSS/GoodbyeDPI](https://github.com/ValdikSS/GoodbyeDPI) |
| AmneziaWG | [amnezia-vpn/amneziawg-windows-client](https://github.com/amnezia-vpn/amneziawg-windows-client) |
| Alice (DPI detection) | [sergey-se/Alice](https://github.com/sergey-se/Alice) |
| Zapret (DPI bypass) | [bol-vap/zapret](https://github.com/bol-vap/zapret) |
| uTLS | [refraction-networking/utls](https://github.com/refraction-networking/utls) |
| Tailscale | [tailscale.com](https://tailscale.com) |
| Headscale | [github.com/juanfont/headscale](https://github.com/juanfont/headscale) |
| NetBird | [netbird.io](https://netbird.io) |
| Gost | [github.com/ginuerzh/gost](https://github.com/ginuerzh/gost) |
| Brook | [github.com/txthinking/brook](https://github.com/txthinking/brook) |
| Rainbow (2026) | Check latest releases |

---

## ⚠️ Legal Disclaimer

This document is for educational and research purposes only. Circumventing
censorship may violate local laws in some jurisdictions (including РФ
under certain interpretations). Know your local regulations before using
these techniques.

---

*Last updated: 2026-05-28. Technologies evolve rapidly — always check
current project repositories for latest releases.*
