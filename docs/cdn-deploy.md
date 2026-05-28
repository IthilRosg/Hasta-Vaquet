# Развёртывание Hasta-Vaquet за Cloudflare CDN

## Полная инструкция для РФ (Май 2026)

Данное руководство описывает развёртывание Hasta-Vaquet VPN-сервера за
Cloudflare CDN для обеспечения устойчивости к DPI (Deep Packet Inspection)
на территории РФ. Основная идея — трафик клиента выглядит как обычный HTTPS
к Cloudflare, а DNS-запросы скрыты за DoH-прокси на Cloudflare Workers.

---

## Схема трафика

```
Клиент → WSS (443) → Cloudflare CDN → nginx → Hasta-Vaquet Server → Internet
Клиент → DoH        → Cloudflare Worker  → 1.1.1.1 / 8.8.8.8
```

**Что видит DPI:**

- TLS 1.3 к IP Cloudflare (не заблокирован)
- SNI = `my-vpn-server.ru` (валидный, не заблокированный домен)
- uTLS Chrome fingerprint (неотличим от настоящего Chrome)
- WebSocket поверх TLS (неотличим от обычного HTTPS)
- DoH к `doh.my-vpn-server.ru` (свой домен, не заблокирован)

---

## Шаг 1: Домен + Cloudflare

1. **Купить домен** у любого регистратора (например, `my-vpn-server.ru`,
   `vpn-123.ru`). Рекомендуется домен без привязки к VPN в названии.

2. **Зарегистрироваться в Cloudflare** (free-тариф подходит).

3. **Добавить домен в Cloudflare** → следовать инструкции по замене NS-записей
   у регистратора на cloudflare-NS.

4. **Дождаться статуса Active** в Cloudflare (обычно 1-30 минут).

5. **Настроить SSL/TLS:**
   - Cloudflare Dashboard → SSL/TLS → Overview
   - Выбрать **Full (strict)**
   - Включить **Always Use HTTPS**

6. **Настроить DNS-записи:**

   | Тип  | Имя                 | Значение          | Proxy |
   |------|---------------------|-------------------|-------|
   | A    | `@`                 | `<IP сервера>`    | ⭕ Да |
   | A    | `doh`               | `<IP сервера>`    | ⭕ Да |
   | CNAME | `www`              | `@`               | ⭕ Да |

   > Оранжевая туча (Proxy) обязательна — именно она прячет реальный IP сервера.

7. **Настроить правила кэширования:**
   - Cloudflare Dashboard → Rules → Page Rules
   - Создать правило:
     - URL: `doh.my-vpn-server.ru/*`
     - Cache Level: **Bypass**
   - Это гарантирует, что DoH-запросы не будут кэшироваться.

---

## Шаг 2: Сервер (VPS)

### Требования к VPS

- **Локация:** за пределами РФ (Нидерланды, Финляндия, Германия, Турция)
- **CPU:** с аппаратным AES-NI (все современные процессоры)
- **Сеть:** 1 Гбит/с и выше
- **ОС:** Ubuntu 22.04 / Debian 12
- **Дополнительно:** защита от DDoS у хостера

### Установка Hasta-Vaquet Server

```bash
# Копирование бинарника сервера
scp server_linux root@<IP>:/usr/local/bin/hasta-vaquet-server
chmod +x /usr/local/bin/hasta-vaquet-server

# Создание конфига
mkdir -p /etc/hasta-vaquet
```

Создать `/etc/hasta-vaquet/server_config.json`:

```json
{
  "port": 19999,
  "routing_salt": "HastaVaquetGlobal",
  "admin_port": 9998,
  "admin_token": "ваш-случайный-токен",
  "admin_path": "/hasta-vaquet",
  "log_file": "/var/log/hasta-vaquet.log",
  "users": [
    {
      "short_id": 1,
      "name": "client-1",
      "secret_key": "...",
      "ip": "10.0.0.10"
    }
  ]
}
```

> `secret_key` генерируется: `openssl rand -hex 32`

### Сертификаты TLS (Cloudflare Origin CA)

Самый простой способ — использовать Cloudflare Origin CA:

```bash
# Установить cloudflare-origin-ca (или скачать через Web-интерфейс)
# Cloudflare Dashboard → SSL/TLS → Origin Server → Create Certificate

# После скачивания:
cp origin-cert.pem /etc/ssl/certs/my-vpn-server.ru.pem
cp origin-key.pem /etc/ssl/private/my-vpn-server.ru.key
```

Или через certbot (если домен уже делегирован):

```bash
apt install certbot
certbot certonly --standalone -d my-vpn-server.ru -d doh.my-vpn-server.ru
```

### Запуск сервера через systemd

Создать `/etc/systemd/system/hasta-vaquet.service`:

```ini
[Unit]
Description=Hasta-Vaquet VPN Server
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/hasta-vaquet-server \
  -config /etc/hasta-vaquet/server_config.json
Restart=always
RestartSec=5
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
```

```bash
systemctl daemon-reload
systemctl enable hasta-vaquet
systemctl start hasta-vaquet
systemctl status hasta-vaquet
```

---

## Шаг 3: Nginx reverse proxy (рекомендуется)

Nginx выступает как TLS-терминатор перед Hasta-Vaquet. Cloudflare
проксирует трафик к nginx (всегда строго через TLS с Origin CA).

### Установка nginx

```bash
apt install nginx-full
# nginx-full включает модуль stream (нужен для прокси)
```

### Конфигурация stream-прокси (WSS)

Создать `/etc/nginx/nginx.conf` (или `/etc/nginx/streams.d/wss.conf`):

```nginx
stream {
    # WSS → Hasta-Vaquet
    server {
        listen 443 ssl;
        # listen 443 quic reuseport;  # QUIC через nginx (если нужно)

        ssl_certificate     /etc/ssl/certs/my-vpn-server.ru.pem;
        ssl_certificate_key /etc/ssl/private/my-vpn-server.ru.key;

        ssl_protocols TLSv1.2 TLSv1.3;
        ssl_ciphers HIGH:!aNULL:!MD5;
        ssl_prefer_server_ciphers on;

        # Прокси TCP-трафика на Hasta-Vaquet
        proxy_pass 127.0.0.1:19999;
        proxy_buffer_size 16k;
    }
}
```

> **Важно:** Cloudflare проксирует на nginx по HTTPS (port 443).
> Hasta-Vaquet принимает UDP на `127.0.0.1:19999` уже без TLS (TLS снят nginx).

### Проверка nginx

```bash
nginx -t
systemctl restart nginx
```

### (Опционально) QUIC через nginx

```nginx
server {
    listen 443 quic reuseport;

    ssl_certificate     /etc/ssl/certs/my-vpn-server.ru.pem;
    ssl_certificate_key /etc/ssl/private/my-vpn-server.ru.key;

    # QUIC + TCP fallback
    proxy_pass 127.0.0.1:19999;
}
```

---

## Шаг 4: DoH Worker (Cloudflare Worker)

1. **Перейти** в Cloudflare Dashboard → Workers & Pages → Create Application
   → Create Worker.

2. **Назвать** `doh-proxy`.

3. **Вставить содержимое** `docs/doh-worker.js` (из репозитория).

4. **Нажать Deploy.**

5. **Привязать маршрут:**
   - Workers & Pages → `doh-proxy` → Triggers → Routes
   - Add route: `doh.my-vpn-server.ru/dns-query`
   - Выбрать зону `my-vpn-server.ru`

6. **Проверить:**
   ```bash
   # GET-запрос
   curl -H "Accept: application/dns-message" \
     "https://doh.my-vpn-server.ru/dns-query?dns=AAABAAABAAAAAAAAA3d3dwdleGFtcGxlA2NvbQAAAQAB"

   # POST-запрос с сырым DNS-сообщением
   curl -H "Content-Type: application/dns-message" \
     -H "Accept: application/dns-message" \
     --data-binary @dns-query.bin \
     "https://doh.my-vpn-server.ru/dns-query"
   ```

### Мониторинг Worker

Cloudflare Dashboard покажет:
- Requests per day
- Errors
- CPU time
- Status codes (200/429/502/400)

Для детального логирования включить Logpush → R2 / Grafana.

---

## Шаг 5: Клиент

### config.json

```json
{
  "server_ip": "my-vpn-server.ru",
  "port": 443,
  "transport": "wss",
  "short_id": 1,
  "secret_key": "...",
  "routing_salt": "HastaVaquetGlobal",
  "internal_ip": "10.0.0.10",
  "dns": "https://doh.my-vpn-server.ru/dns-query",
  "fec": 1,
  "mtu": 1300
}
```

### Настройки

| Поле | Значение | Пояснение |
|------|----------|-----------|
| `server_ip` | `my-vpn-server.ru` | Домен за Cloudflare |
| `port` | `443` | Стандартный HTTPS-порт |
| `transport` | `wss` / `auto` | `wss` — WebSocket в TLS; `auto` — выбор по приоритету |
| `dns` | `https://doh.my-vpn-server.ru/dns-query` | DoH через свой Worker |
| `fec` | `1` | Без дублирования (если потери <10%) |
| `mtu` | `1300` | Баланс скорости и фрагментации |

### Подключение

```bash
# Windows (CLI)
hasta-vaquet.exe -config config.json

# Windows (GUI) — импортировать config.json через меню
```

---

## Проверка обхода DPI

```bash
# 1. Проверить, что SNI виден только как Cloudflare
curl -v https://my-vpn-server.ru 2>&1 | grep "Server certificate"

# 2. Проверить DoH
dig @doh.my-vpn-server.ru google.com +https

# 3. Проверить, что реальный IP скрыт
# Сделать запрос с сервера (или через клиент) на checkip.amazonaws.com
curl -s https://checkip.amazonaws.com
# Должен вернуть IP Cloudflare, не ваш сервер

# 4. Проверить отсутствие утечки DNS
# На Windows: ipconfig /displaydns | findstr my-vpn-server.ru
# Должны быть только DoH-запросы, не в открытую
```

---

## Устранение неполадок

| Проблема | Причина | Решение |
|----------|---------|---------|
| Cloudflare 522 | Сервер недоступен | Проверить that nginx запущен и порт 443 открыт |
| Cloudflare 526 | Невалидный SSL-сертификат | Проверить Origin CA / certbot |
| DoH 502 | Upstream DNS недоступен | Проверить, что 1.1.1.1 и 8.8.8.8 доступны с Worker |
| DoH 429 | Превышен rate limit | Подождать 1 минуту |
| Низкая скорость | FEC > 1 без потерь | Установить FEC=1 |
| Разрывы соединения | NAT timeout | Убедиться, что keepalive включён |
| DNS-утечка | Системный DNS не DoH | Настроить DoH в config.json |

---

## Дополнительные рекомендации

- **Использовать Wireshark для проверки:** весь трафик должен быть TLS 1.3
  с SNI вашего домена. Никаких DNS-запросов в открытую.
- **Регулярно менять домен** при подозрении на блокировку.
- **Cloudflare Argo Tunnel (cloudflared)** — альтернатива, если нельзя открыть
  порт на сервере. Туннель инициируется сервером наружу.
- **Для РФ:** рекомендуется домен .ru или .com — они реже блокируются
  целиком, чем специализированные TLD. Не используйте слова vpn/proxy/tunnel
  в имени домена.
