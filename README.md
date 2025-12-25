<!-- Краткий README для русской версии с отсылкой к документации -->

## Fortio

Fortio (Φορτίο) — это многофункциональный инструмент нагрузочного тестирования и библиотека на Go.
Он умеет генерировать нагрузку по HTTP(S), gRPC, TCP, UDP, а также по Kafka (через клиент
[franz-go](https://github.com/twmb/franz-go)), и предоставляет:

- простой **CLI**,
- **веб‑UI** и **REST API** для запуска и анализа тестов,
- встроенные echo‑сервера и прокси для отладки.

Название Fortio происходит от греческого слова [φορτίο](https://fortio.org/fortio.mp3) — «нагрузка».

### Основные сценарии

- Нагрузочное тестирование HTTP/HTTPS‑сервисов.
- Нагрузочное тестирование gRPC‑сервисов (ping/health и произвольные методы).
- Нагрузка на TCP/UDP echo‑сервисы.
- Нагрузка на Kafka‑топики (producer‑нагрузка) с использованием `franz-go`.
- Просмотр и сравнение результатов через веб‑UI и JSON‑отчёты.

### Установка

- **Docker** (рекомендуется для быстрого старта):

  ```bash
  docker run -p 8080:8080 -p 8079:8079 fortio/fortio server &
  docker run fortio/fortio load http://www.google.com/
  ```

- **Из исходников (Go 1.18+):**

  ```bash
  go install fortio.org/fortio@latest
  # затем:
  fortio server
  ```

- **Готовые бинарники / пакеты**: смотрите раздел *Releases* в GitHub‑репозитории.

После запуска `fortio server` веб‑интерфейс доступен по адресу
`http://localhost:8080/fortio/` (порт и путь можно изменить флагами).

### Краткий пример

```bash
# HTTP‑нагрузка
fortio load -qps 100 -c 10 -t 30s http://localhost:8080/echo

# gRPC‑нагрузка
fortio load -grpc -ping -qps 20 -c 4 -t 10s localhost:8079

# Kafka‑нагрузка
fortio load \
  -kafka-bootstrap "localhost:9092" \
  -kafka-topic "test-topic" \
  -qps 100 -t 30s
```

## Документация по режимам нагрузки

Подробные примеры и флаги для каждого типа нагрузки вынесены в отдельные файлы в каталоге `docs/`:

- **HTTP‑нагрузка**: `docs/http-load.md`
- **gRPC‑нагрузка**: `docs/grpc-load.md`
- **TCP‑нагрузка**: `docs/tcp-load.md`
- **UDP‑нагрузка**: `docs/udp-load.md`
- **Kafka‑нагрузка**: `docs/kafka-load.md`

Рекомендуется начать с HTTP‑раздела, затем по необходимости смотреть gRPC/TCP/UDP/Kafka.

## Веб‑UI и REST API

После запуска `fortio server` доступны:

- веб‑UI: `http://<host>:8080/fortio/` — запуск тестов и просмотр графиков;
- REST API: `http://<host>:8080/fortio/rest/run` и связанные эндпоинты
  (`/rest/status`, `/rest/stop`, `/rest/dns`).

Примеры REST‑запросов есть в соответствующих файлах документации (HTTP/gRPC/TCP/UDP/Kafka).

## Дополнительно

- **Проекты вокруг Fortio:**
  - Fortio Proxy — TLS‑прокси и мультиплексирование gRPC/HTTP.
  - Fortiotel — интеграция с OpenTelemetry.
- **Встроенный язык сценариев**: `fortio script` (на базе [grol](https://grol.io/)).

Расширенную англоязычную документацию, FAQ и исторические примеры можно найти в wiki
и старых версиях `README.md` в репозитории GitHub.

