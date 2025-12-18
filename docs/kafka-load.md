## Kafka-нагрузка

Эта секция описывает добавленную поддержку нагрузки через Kafka, реализованную на базе клиента [franz-go](https://github.com/twmb/franz-go).

Fortio в режиме Kafka выступает **производителем** (producer) и отправляет сообщения в заданный Kafka‑топик с заданным QPS и числом потоков. Сервис‑потребитель читает эти сообщения и на своей стороне собирает метрики. 

Fortio может собирать и выводить метрики:
- **Метрики Kafka‑клиента** (опционально, флаг `-kafka-metrics`): агрегированные метрики производителя (producer).
- **Метрики сервиса‑потребителя** (опционально, флаг `-kafka-consumer-metrics-url`): метрики сервиса, который читает сообщения из Kafka. Fortio запрашивает метрики в формате Prometheus с указанного URL после завершения теста.

### Варианты использования

- CLI: `fortio load` с Kafka‑флагами.
- Веб‑UI: выбор режима **kafka** в форме.
- REST API: `/fortio/rest/run` с `runner=kafka` или Kafka‑URL.

### CLI: основные флаги для Kafka

Новая функциональность реализована поверх существующей команды `load`.

Дополнительные флаги:

- **`-kafka-bootstrap "<list>"`**  
  Список Kafka bootstrap‑серверов через запятую.  
  Примеры:
  - `-kafka-bootstrap "localhost:9092"`
  - `-kafka-bootstrap "kafka-1:9092,kafka-2:9092"`

- **`-kafka-topic "<topic>"`**  
  Топик, в который Fortio будет отправлять сообщения.

- **`-kafka-metrics`**  
  Включить сбор и вывод агрегированных метрик Kafka‑клиента:
  - общее число запросов produce,
  - число успешных/ошибочных запросов,
  - общий объём отправленных байт,
  - средняя и максимальная задержка отправки.

- **`-kafka-consumer-metrics-url "<name> <url>"`**  
  URL сервиса‑потребителя для сбора метрик. Можно указать несколько сервисов, повторив флаг.  
  Формат: `"<имя_сервиса> <url>"`.  
  Примеры:
  - `-kafka-consumer-metrics-url "service1 http://testservice:8080/metrics"`
  - `-kafka-consumer-metrics-url "worker1 http://localhost:8080/metrics" -kafka-consumer-metrics-url "worker2 http://localhost:8081/metrics"`
  
  Если URL не содержит путь `/metrics`, он будет добавлен автоматически.  
  **Важно**: Сервис должен предоставлять метрики в формате Prometheus на указанном endpoint.  
  **Несколько сервисов**: Можно указать несколько флагов для сбора метрик с разных сервисов одновременно.

Остальные флаги (`-qps`, `-c`, `-t`, `-n`, `-payload`, `-payload-file`, `-payload-size` и т.д.) работают аналогично HTTP‑нагрузке, но payload используется как тело Kafka‑сообщения.

### CLI: примеры

**Минимальный пример:**

```bash
fortio load \
  -kafka-bootstrap "localhost:9092" \
  -kafka-topic "test-topic" \
  -qps 100 -t 30s
```

**С пользовательским payload:**

```bash
fortio load \
  -kafka-bootstrap "localhost:9092" \
  -kafka-topic "events" \
  -payload '{"event":"test","value":123}' \
  -qps 500 -c 10 -t 60s
```

**Фиксированное количество сообщений без ограничения по времени:**

```bash
fortio load \
  -kafka-bootstrap "kafka-1:9092,kafka-2:9092" \
  -kafka-topic "load-topic" \
  -payload-file msg.json \
  -qps -1 -n 100000
```

**Со сбором метрик Kafka:**

```bash
fortio load \
  -kafka-bootstrap "localhost:9092" \
  -kafka-topic "metrics-topic" \
  -kafka-metrics \
  -qps 1000 -c 20 -t 120s
```

**Со сбором метрик одного сервиса‑потребителя:**

```bash
fortio load \
  -kafka-bootstrap "localhost:9092" \
  -kafka-topic "test-topic" \
  -kafka-consumer-metrics-url "testservice http://testservice:8080/metrics" \
  -qps 100 -c 4 -t 30s
```

**Со сбором метрик нескольких сервисов‑потребителей:**

```bash
fortio load \
  -kafka-bootstrap "localhost:9092" \
  -kafka-topic "test-topic" \
  -kafka-consumer-metrics-url "worker1 http://worker1:8080/metrics" \
  -kafka-consumer-metrics-url "worker2 http://worker2:8080/metrics" \
  -kafka-consumer-metrics-url "aggregator http://aggregator:8080/metrics" \
  -qps 100 -c 4 -t 30s
```

**С метриками Kafka и нескольких сервисов‑потребителей:**

```bash
fortio load \
  -kafka-bootstrap "localhost:9092" \
  -kafka-topic "test-topic" \
  -kafka-metrics \
  -kafka-consumer-metrics-url "service1 http://service1:8080/metrics" \
  -kafka-consumer-metrics-url "service2 http://service2:8080/metrics" \
  -qps 500 -c 10 -t 60s
```

### Веб‑UI (Kafka)

1. Запустить сервер:

   ```bash
   fortio server
   ```

2. Открыть `http://localhost:8080/fortio/`.
3. В форме:
   - **URL** можно оставить пустым (для Kafka он не обязателен).
   - В блоке **Load using** выбрать **`kafka`**.
   - Заполнить:
     - `bootstrap servers` (например `localhost:9092` или `kafka-1:9092,kafka-2:9092`),
     - `topic` (обязателен),
     - опционально `collect kafka metrics`,
     - опционально добавить один или несколько **Consumer Service** — указать имя сервиса и URL метрик (например `worker1` и `http://worker1:8080/metrics`).
   - При необходимости задать `Payload` (будет телом Kafka‑сообщения).
   - Настроить `QPS`, `Duration`, `Threads` и т.д.
4. Нажать **Start** и затем просмотреть результат и графики. Метрики каждого consumer‑сервиса отображаются в отдельных секциях с именем сервиса.

UI автоматически:

- скрывает поле URL при выборе режима **kafka**,
- делает поля bootstrap/topic обязательными.

### REST API (Kafka)

REST‑эндпоинт такой же, как для других режимов:

`POST http://<host>:8080/fortio/rest/run`

**Пример 1 — явный `runner=kafka`:**

```bash
curl -s -d '{
  "runner": "kafka",
  "kafka-bootstrap": "localhost:9092",
  "kafka-topic": "rest-topic",
  "kafka-metrics": "on",
  "qps": "100",
  "c": "4",
  "t": "30s",
  "payload": "rest-message"
}' "http://localhost:8080/fortio/rest/run" | jq
```

**Пример 2 — с несколькими consumer‑сервисами:**

```bash
curl -s -d '{
  "runner": "kafka",
  "kafka-bootstrap": "localhost:9092",
  "kafka-topic": "rest-topic",
  "kafka-metrics": "on",
  "kafka-consumer-services": [
    {"name": "worker1", "url": "http://worker1:8080/metrics"},
    {"name": "worker2", "url": "http://worker2:8080/metrics"}
  ],
  "qps": "100",
  "c": "4",
  "t": "30s"
}' "http://localhost:8080/fortio/rest/run" | jq
```

**Пример 3 — использование Kafka‑URL:**

```bash
curl -s -d '{
  "url": "kafka://localhost:9092/rest-topic",
  "qps": "50",
  "c": "2",
  "t": "60s"
}' "http://localhost:8080/fortio/rest/run" | jq
```

В этом случае bootstrap и topic могут быть извлечены из `url`, но при необходимости их можно продублировать полями `kafka-bootstrap` и `kafka-topic`.

### Как это работает внутри

- Используется Kafka‑клиент [franz-go](https://github.com/twmb/franz-go), пакеты `kgo` и `kadm` (`github.com/twmb/franz-go/pkg/kgo`, `github.com/twmb/franz-go/pkg/kadm`) — см. репозиторий проекта `franz-go` для деталей по конфигурации клиента.

**Валидация подключения:**

Перед началом теста Fortio выполняет проверку подключения к Kafka:
1. Проверяет доступность bootstrap серверов (ping)
2. Проверяет существование указанного топика

Если подключение не удалось или топик не существует, тест **прерывается с ошибкой**, не начиная отправку сообщений. Это позволяет быстро выявить проблемы конфигурации.

Примеры ошибок:
- `kafka connection validation failed: failed to connect to Kafka brokers: ...` — не удалось подключиться к bootstrap серверам
- `kafka connection validation failed: topic "test" does not exist` — указанный топик не существует

- Для каждого потока создаётся собственный Kafka‑клиент, который синхронно отправляет сообщения и собирает локальную статистику.
- Fortio агрегирует результаты:
  - количество отправленных сообщений,
  - байты,
  - коды результатов (успех/ошибка),
  - при включенных `-kafka-metrics` — дополнительные метрики задержек и объёмов Kafka‑клиента,
  - при указанном `-kafka-consumer-metrics-url` — метрики сервиса‑потребителя в формате Prometheus.

**Сбор метрик сервисов‑потребителей:**

Во время теста (в UI) или после завершения теста (в CLI) Fortio отправляет HTTP GET запросы на указанные URL (обычно `/metrics` endpoint сервисов) и собирает метрики в формате Prometheus. Это позволяет анализировать производительность и состояние сервисов, которые обрабатывают сообщения из Kafka.

**Поддержка нескольких сервисов:**

Fortio поддерживает сбор метрик с нескольких сервисов одновременно. Каждый сервис идентифицируется уникальным именем, которое задаётся пользователем. В CLI результаты выводятся отдельно для каждого сервиса с указанием его имени. В UI метрики каждого сервиса отображаются в отдельных секциях.

**Примечание для Fission Lambda функций:**

Для lambda функций в Fission с `executortype=nedeploy` указывайте URL контейнера, который выполняет функцию (не fetcher). Fortio соберёт метрики именно этого контейнера.


