## gRPC-нагрузка

### Основное

Fortio поддерживает нагрузочное тестирование gRPC‑сервисов:

- через специальную команду **`grpcping`** (ping/health),
- через **`fortio load -grpc`** (нагрузка на gRPC‑метод),
- через веб‑UI и REST API.

### Ключевые флаги для gRPC

- **`-grpc`**: включить gRPC‑режим для `load`.
- **`-ping`**: использовать ping‑метод вместо health‑чека.
- **`-grpc-ping-delay <dur>`**: искусственная задержка ответа в ping‑сервисе.
- **`-s <streams>`**: количество gRPC‑потоков (streams) на соединение.
- **`-cacert`**, **`-cert`**, **`-key`**: TLS‑сертификаты.
- **`-h2`** или префикс `https://` для TLS к внешним gRPC‑сервисам.

Остальные флаги (`-qps`, `-c`, `-t`, `-n`, `-payload` и др.) работают так же, как для HTTP.

### Примеры CLI

**Простой ping через отдельную команду:**

```bash
fortio grpcping -n 5 localhost
```

**Нагрузка на gRPC‑ping с задержкой и payload:**

```bash
fortio load -a -grpc -ping \
  -grpc-ping-delay 250ms \
  -payload "01234567890" \
  -c 2 -s 4 \
  https://fortio-stage.istio.io
```

**Нагрузка на произвольный gRPC‑метод (reflection):**

```bash
fortio load -grpc \
  -grpc-method "my.service.v1.Service/Method" \
  -payload '{"field":"value"}' \
  -H "Content-Type: application/grpc+json" \
  -qps 50 -c 4 -t 30s \
  localhost:8079
```

### Веб‑UI

1. Запускаем сервер:

   ```bash
   fortio server
   ```

2. Открываем `http://localhost:8080/fortio/`.
3. В блоке **Load using** выбираем **`grpc`**.
4. Указываем:
   - адрес gRPC‑сервера (host:port),
   - `grpc secure transport (tls)` при необходимости,
   - `ping` / `healthservice` / `grpc-method` (в REST слое).

5. Настраиваем **QPS**, **Duration**, **Threads**, **Payload**, жмём **Start**.

### REST API (gRPC)

Пример запуска gRPC‑нагрузки через REST:

```bash
curl -s -d '{"url":"localhost:8079","grpc":"on","ping":"on","qps":"20","c":"4","t":"10s"}' \
  "http://localhost:8080/fortio/rest/run" | jq
```

Основные поля:

- `url` — gRPC‑endpoint (обычно host:port).
- `grpc: "on"` — режим gRPC.
- `ping: "on"` или `healthservice` / `grpc-method`.
- остальные поля — как в HTTP‑режиме.


