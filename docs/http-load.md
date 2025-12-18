## HTTP-нагрузка (HTTP/HTTPS)

### Основное

Fortio может генерировать HTTP(S)-нагрузку как через CLI, так и через веб‑UI и REST API.

Базовый пример:

```bash
fortio load http://localhost:8080/echo
```

### Ключевые флаги для HTTP

- **`-qps <rate>`**: целевой QPS (0 — максимальная скорость без ожиданий).
- **`-c <connections>`**: количество параллельных соединений/горутин.
- **`-t <duration>`**: длительность теста, например `-t 30s`, `-t 5m`, `-t 1h`.
- **`-n <calls>`**: вместо `-t` — ровно N запросов.
- **`-payload <str>` / `-payload-file <file>` / `-payload-size <bytes>`**: тело запроса (POST).
- **`-H "Header: Value"`**: дополнительные заголовки (можно несколько раз).
- **`-timeout <dur>`**: таймаут запроса (по умолчанию ~3s).
- **`-a`**: автоматически сохранять JSON‑результат в файл.
- **`-json <path>`**: путь к JSON‑файлу или `-` для stdout.
- **`-h2`**: попытка использовать HTTP/2 (включает `-stdclient`).
- **`-https-insecure` / `-k`**: не проверять TLS‑сертификаты.

Подробный список всех флагов — см. раздел **Command line flags** в `README.md`.

### Примеры

**Простой тест:**

```bash
fortio load -qps 100 -c 10 -t 30s http://localhost:8080/echo
```

**HTTP/2 + TLS:**

```bash
fortio load -h2 https://example.com/
```

**С телом POST из файла:**

```bash
fortio load -payload-file body.json -H "Content-Type: application/json" https://api.example.com/v1/resource
```

### Веб‑UI (порт по умолчанию 8080)

1. Запустить сервер:

   ```bash
   fortio server
   ```

2. Открыть UI: `http://localhost:8080/fortio/`.

3. В форме:
   - Указать **URL**.
   - Настроить **QPS**, **Duration**, **Threads**, **Payload**, заголовки.
   - Оставить выбранным `tcp/udp/http` в блоке **Load using**.

4. Нажать **Start** и затем просмотреть графики и сохранённые результаты.

### REST API (HTTP)

Тот же HTTP‑тест можно запустить через REST:

```bash
curl -s -d '{"url":"http://localhost:8080/echo","qps":"50","c":"4","t":"10s"}' \
  "http://localhost:8080/fortio/rest/run" | jq
```

Поля:

- `url` — целевой HTTP(S) URL.
- `qps`, `c`, `t`, `n`, `payload`, `headers`, `save`, `jsonPath` и др. — аналогично CLI/UI.


