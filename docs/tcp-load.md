## TCP-нагрузка

### Основное

Fortio умеет генерировать нагрузку на TCP‑сервисы (например, echo‑серверы).

TCP‑нагрузка включается указанием префикса `tcp://` в целевом адресе:

```bash
fortio load tcp://localhost:8078
```

### Ключевые флаги для TCP

- **`tcp://host:port`** — включает TCP‑runner.
- **`-qps <rate>`**, **`-c <connections>`**, **`-t <duration>`**, **`-n <calls>`** — общие флаги нагрузки.
- **`-payload <str>` / `-payload-file <file>`** — полезная нагрузка, которая будет отправляться по TCP и ожидаться в ответ (echo).
- **`-timeout <dur>`** — таймаут TCP‑операций.

### Примеры

**Запуск TCP‑echo‑сервера и нагрузка:**

```bash
# сервер
fortio tcp-echo &

# нагрузка
fortio load -qps -1 -n 100000 tcp://localhost:8078
```

**TCP‑нагрузка с кастомным payload:**

```bash
fortio load -qps 500 -c 10 -t 60s \
  -payload "hello over tcp" \
  tcp://my-tcp-service:9000
```

### Веб‑UI

1. Запустить `fortio server`.
2. Открыть `http://localhost:8080/fortio/`.
3. В поле **URL** указать `tcp://host:port`.
4. В блоке **Load using** оставить выбранным `tcp/udp/http`.
5. Задать QPS, длительность, число потоков, payload (если нужен), нажать **Start**.

### REST API (TCP)

Пример:

```bash
curl -s -d '{"url":"tcp://localhost:8078","qps":"1000","c":"4","n":"50000"}' \
  "http://localhost:8080/fortio/rest/run" | jq
```

Поле `url` с префиксом `tcp://` автоматически включает TCP‑runner.


