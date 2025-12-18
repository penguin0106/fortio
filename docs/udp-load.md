## UDP-нагрузка

### Основное

Для тестирования UDP‑сервисов Fortio использует специальный UDP‑echo‑клиент.

UDP‑нагрузка включается префиксом `udp://` в целевом URL:

```bash
fortio load udp://localhost:8078/
```

### Ключевые флаги для UDP

- **`udp://host:port`** — включает UDP‑runner.
- **`-qps <rate>`**, **`-c <connections>`**, **`-t <duration>`**, **`-n <calls>`** — общие флаги.
- **`-payload <str>` / `-payload-file <file>`** — полезная нагрузка (ожидается echo‑ответ того же размера).
- **`-udp-timeout <dur>`** — таймаут для UDP‑ответов.
- **`-udp-async`** (для сервера) — асинхронная обработка ответов echo‑сервера.

### Примеры

**Запустить UDP‑echo‑сервер и нагрузку:**

```bash
# сервер
fortio udp-echo &

# нагрузка
fortio load -qps -1 -n 100000 udp://localhost:8078/
```

**UDP‑нагрузка с payload:**

```bash
fortio load -qps 2000 -c 8 -t 30s \
  -payload "udp-payload" \
  udp://my-udp-service:9000/
```

### Веб‑UI

1. `fortio server`
2. Открыть `http://localhost:8080/fortio/`.
3. В **URL** указать `udp://host:port/`.
4. В блоке **Load using** оставить `tcp/udp/http`.
5. Настроить параметры нагрузки и нажать **Start**.

### REST API (UDP)

```bash
curl -s -d '{"url":"udp://localhost:8078/","qps":"500","c":"4","n":"100000"}' \
  "http://localhost:8080/fortio/rest/run" | jq
```

Поле `url` с префиксом `udp://` включает UDP‑runner.


