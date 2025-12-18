## Kafka-нагрузка

Эта секция описывает добавленную поддержку нагрузки через Kafka, реализованную на базе клиента [franz-go](https://github.com/twmb/franz-go).

Fortio в режиме Kafka выступает **производителем** (producer) и отправляет сообщения в заданный Kafka‑топик с заданным QPS и числом потоков. Сервис‑потребитель читает эти сообщения и на своей стороне собирает метрики. Дополнительно Fortio может выводить агрегированные метрики Kafka‑клиента.

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
     - опционально `collect kafka metrics`.
   - При необходимости задать `Payload` (будет телом Kafka‑сообщения).
   - Настроить `QPS`, `Duration`, `Threads` и т.д.
4. Нажать **Start** и затем просмотреть результат и графики.

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

**Пример 2 — использование Kafka‑URL:**

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

- Используется Kafka‑клиент [franz-go](https://github.com/twmb/franz-go), пакет `kgo` (`github.com/twmb/franz-go/pkg/kgo`) — см. репозиторий проекта `franz-go` для деталей по конфигурации клиента.
- Для каждого потока создаётся собственный Kafka‑клиент, который синхронно отправляет сообщения и собирает локальную статистику.
- Fortio агрегирует результаты:
  - количество отправленных сообщений,
  - байты,
  - коды результатов (успех/ошибка),
  - при включенных `-kafka-metrics` — дополнительные метрики задержек и объёмов.


