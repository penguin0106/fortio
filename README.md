<!-- Краткий README для русской версии с отсылкой к документации -->

## Fortio

Fortio (Φορτίο) — это многофункциональный инструмент нагрузочного тестирования и библиотека на Go.
Он умеет генерировать нагрузку по HTTP(S), gRPC, TCP, UDP, а также по Kafka (через клиент
[franz-go](https://github.com/twmb/franz-go)), и предоставляет:

- простой **CLI**,
- **веб‑UI** и **REST API** для запуска и анализа тестов,
- встроенные echo‑сервера и прокси для отладки,
- **real-time мониторинг** метрик Kafka и Consumer-сервисов,
- **авто-определение IP Lambda-функций** в Kubernetes.

Название Fortio происходит от греческого слова [φορτίο](https://fortio.org/fortio.mp3) — «нагрузка».

### Основные сценарии

- Нагрузочное тестирование HTTP/HTTPS‑сервисов.
- Нагрузочное тестирование gRPC‑сервисов (ping/health и произвольные методы).
- Нагрузка на TCP/UDP echo‑сервисы.
- Нагрузка на Kafka‑топики (producer‑нагрузка) с использованием `franz-go`.
- Мониторинг метрик Consumer-сервисов и Lambda-функций в реальном времени.
- Просмотр и сравнение результатов через веб‑UI и JSON‑отчёты.

---

## Структура проекта

```
fortio/
├── cmd/
│   └── fortio/          # Точка входа (main.go)
├── pkg/                 # Публичные пакеты (можно импортировать)
│   ├── fhttp/           # HTTP runner и клиент
│   ├── fgrpc/           # gRPC runner
│   ├── fnet/            # Сетевые утилиты
│   ├── periodic/        # Периодический runner (ядро)
│   ├── stats/           # Статистика и гистограммы
│   ├── tcprunner/       # TCP runner
│   ├── udprunner/       # UDP runner
│   ├── kafkarunner/     # Kafka runner
│   ├── rapi/            # REST API
│   └── version/         # Версия
├── internal/            # Внутренние пакеты
│   ├── cli/             # CLI логика
│   ├── ui/              # Веб-интерфейс
│   ├── bincommon/       # Общие флаги
│   └── ...
└── docs/                # Документация
```

---

## Сборка и установка

### Из исходников (Go 1.21+)

```bash
# Клонируйте репозиторий
git clone https://github.com/fortio/fortio.git
cd fortio

# Соберите бинарник
go build -o fortio ./cmd/fortio/

# Или установите глобально
go install ./cmd/fortio/
```

### Docker

```bash
# Соберите образ
docker build -t fortio:latest .

# Или используйте готовый образ
docker pull fortio/fortio
```

### Готовые бинарники

Смотрите раздел **Releases** в GitHub-репозитории для загрузки готовых бинарников под вашу платформу.

---

## Запуск

### Локально (CLI)

```bash
# Запуск веб-сервера
./fortio server

# Или с указанием порта
./fortio server -http-port 8080
```

После запуска веб‑интерфейс доступен по адресу: `http://localhost:8080/fortio/`

### Docker

```bash
# Запуск сервера
docker run -p 8080:8080 -p 8079:8079 fortio/fortio server

# С монтированием данных для сохранения результатов
docker run -p 8080:8080 -v $(pwd)/data:/var/lib/fortio fortio/fortio server

# С ENV переменными
docker run -p 8080:8080 \
  -e FUNCTION_NAMESPACE=openfaas-fn \
  fortio/fortio server
```

### Kubernetes

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: fortio
spec:
  replicas: 1
  selector:
    matchLabels:
      app: fortio
  template:
    metadata:
      labels:
        app: fortio
    spec:
      serviceAccountName: fortio  # Требуется для авто-определения Lambda-функций
      containers:
      - name: fortio
        image: fortio/fortio:latest
        ports:
        - containerPort: 8080
        - containerPort: 8079
        env:
        - name: FUNCTION_NAMESPACE
          value: "openfaas-fn"
```

---

## Переменные окружения (ENV)

| Переменная | Описание | Значение по умолчанию |
|------------|----------|----------------------|
| `FUNCTION_NAMESPACE` | Kubernetes namespace для поиска Lambda-функций при авто-определении IP | `default` |

### Пример использования

```bash
# Linux/macOS
export FUNCTION_NAMESPACE=openfaas-fn
./fortio server

# Windows PowerShell
$env:FUNCTION_NAMESPACE="openfaas-fn"
.\fortio.exe server

# Windows CMD
set FUNCTION_NAMESPACE=openfaas-fn
fortio.exe server
```

### Авто-определение Lambda-функций

Fortio может автоматически находить IP-адреса подов Lambda-функций в Kubernetes. Для этого:

1. Fortio должен быть запущен внутри Kubernetes кластера
2. ServiceAccount должен иметь права на чтение подов:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: fortio-pod-reader
rules:
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get", "list"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: fortio-pod-reader-binding
subjects:
- kind: ServiceAccount
  name: fortio
  namespace: default
roleRef:
  kind: ClusterRole
  name: fortio-pod-reader
  apiGroup: rbac.authorization.k8s.io
```

3. Укажите `FUNCTION_NAMESPACE` — namespace где находятся Lambda-функции
4. В UI выберите **"+ Lambda функция"** и включите флаг **"Авто"**

Fortio будет искать поды по лейблам:
- `function=<имя_функции>`
- `faas_function=<имя_функции>`
- `app=<имя_функции>`
- `app.kubernetes.io/name=<имя_функции>`

Порт метрик по умолчанию: **8888**

---

## Примеры использования

### HTTP-нагрузка

```bash
# Базовый тест
fortio load -qps 100 -c 10 -t 30s http://localhost:8080/echo

# С сохранением результатов
fortio load -qps 100 -c 10 -t 30s -json result.json http://localhost:8080/api
```

### gRPC-нагрузка

```bash
fortio load -grpc -ping -qps 20 -c 4 -t 10s localhost:8079
```

### Kafka-нагрузка

```bash
fortio load \
  -kafka-bootstrap "localhost:9092" \
  -kafka-topic "test-topic" \
  -qps 100 -t 30s
```

### Через веб-UI

1. Откройте `http://localhost:8080/fortio/`
2. Выберите тип теста (HTTP, gRPC, TCP, UDP, Kafka)
3. Настройте параметры
4. Добавьте Consumer Metrics Sources (опционально):
   - **Сервис** — укажите URL метрик напрямую
   - **Lambda функция** — укажите имя функции, включите "Авто" для автоматического определения IP
5. Нажмите **Start**
6. Наблюдайте метрики в реальном времени

---

## Документация

Подробные примеры и флаги для каждого типа нагрузки:

| Тип нагрузки | Документация |
|--------------|--------------|
| HTTP/HTTPS | [`docs/http-load.md`](docs/http-load.md) |
| gRPC | [`docs/grpc-load.md`](docs/grpc-load.md) |
| TCP | [`docs/tcp-load.md`](docs/tcp-load.md) |
| UDP | [`docs/udp-load.md`](docs/udp-load.md) |
| Kafka | [`docs/kafka-load.md`](docs/kafka-load.md) |

---

## Веб-UI и REST API

После запуска `fortio server` доступны:

| Endpoint | Описание |
|----------|----------|
| `http://<host>:8080/fortio/` | Веб-UI для запуска тестов |
| `http://<host>:8080/fortio/browse` | Просмотр сохранённых результатов |
| `http://<host>:8080/fortio/rest/run` | REST API для запуска тестов |
| `http://<host>:8080/fortio/rest/status` | Статус текущих тестов |
| `http://<host>:8080/fortio/rest/stop` | Остановка теста |

### Real-time мониторинг

При запуске теста через UI доступен мониторинг в реальном времени:
- **Kafka Metrics** — QPS, Latency, Messages, Success/Errors
- **Consumer Services** — метрики из Prometheus endpoints сервисов
- **Lambda Functions** — метрики из Lambda-функций (автоматическое определение IP)

---

## Флаги командной строки

### Основные флаги

| Флаг | Описание | По умолчанию |
|------|----------|--------------|
| `-http-port` | Порт HTTP-сервера | `8080` |
| `-grpc-port` | Порт gRPC-сервера | `8079` |
| `-data-dir` | Директория для сохранения результатов | `./data` |
| `-qps` | Запросов в секунду | `8` |
| `-c` | Количество параллельных соединений | `4` |
| `-t` | Длительность теста | `5s` |
| `-n` | Количество запросов (вместо времени) | - |

### Kafka-специфичные флаги

| Флаг | Описание |
|------|----------|
| `-kafka-bootstrap` | Адрес Kafka брокера |
| `-kafka-topic` | Название топика |
| `-kafka-sasl-user` | SASL пользователь |
| `-kafka-sasl-password` | SASL пароль |

Полный список флагов: `fortio help` или `fortio load -h`

---

## Кастомный логгер (slog)

Fortio использует собственный логгер на базе `log/slog`. Вы можете настроить его через функциональные опции.

### Пример инициализации логгера

```go
package main

import (
    "os"

    "fortio.org/fortio/internal/bincommon"
    "fortio.org/fortio/internal/cli"
    "fortio.org/fortio/pkg/log"
)

func main() {
    os.Exit(cli.FortioMainWithConfig(&bincommon.FortioConfig{
        LoggerSetup: func() {
            logger := initLogger()
            logger.SetAsDefault()
        },
    }))
}

func initLogger() *log.Logger {
    logName := "myapp"
    logRelease := "1.0.0"
    logEnvironment := "local"
    logLevel := "INFO"

    if os.Getenv("LOGGER_NAME") != "" {
        logName = os.Getenv("LOGGER_NAME")
    }
    if os.Getenv("RELEASE") != "" {
        logRelease = os.Getenv("RELEASE")
    }
    if os.Getenv("ENVIRONMENT") != "" {
        logEnvironment = os.Getenv("ENVIRONMENT")
    }
    if os.Getenv("LOG_LEVEL") != "" {
        logLevel = os.Getenv("LOG_LEVEL")
    }

    return log.New(
        log.WithName(logName),
        log.WithRelease(logRelease),
        log.WithEnvironment(logEnvironment),
        log.WithLevelString(logLevel),
    )
}
```

### Опции логгера

| Опция | Описание |
|-------|----------|
| `log.WithName(string)` | Имя логгера/приложения |
| `log.WithRelease(string)` | Версия релиза |
| `log.WithEnvironment(string)` | Окружение (local, dev, prod) |
| `log.WithLevel(log.Level)` | Уровень логирования |
| `log.WithLevelString(string)` | Уровень из строки (DEBUG, INFO, WARN, ERROR) |
| `log.WithOutput(io.Writer)` | Вывод (по умолчанию stderr) |
| `log.WithHandler(slog.Handler)` | Кастомный slog.Handler |

### API пакета pkg/log

| Функция | Описание |
|---------|----------|
| `log.New(opts...)` | Создаёт новый Logger с опциями |
| `log.SetDefault(slog.Logger)` | Устанавливает slog.Logger как глобальный |
| `log.SetLevel(log.Level)` | Устанавливает уровень логирования |
| `log.Infof/Debugf/Warnf/Errf` | Форматированное логирование |
| `log.S(level, msg, attrs...)` | Structured logging с атрибутами |
| `log.Slog()` | Возвращает slog.Logger для прямого доступа |

### Уровни логирования

```go
log.Debug    // 0 - отладка
log.Verbose  // 1 - подробно  
log.Info     // 2 - информация (default)
log.Warning  // 3 - предупреждения
log.Error    // 4 - ошибки
log.Critical // 5 - критические
log.Fatal    // 6 - фатальные
```

### ENV переменные для логгера

| Переменная | Описание | По умолчанию |
|------------|----------|--------------|
| `LOGGER_NAME` | Имя логгера | `fortio` |
| `RELEASE` | Версия релиза | `0.0.0` |
| `ENVIRONMENT` | Окружение | `local` |
| `LOG_LEVEL` | Уровень логирования | `INFO` |

---

## Дополнительно

- **Проекты вокруг Fortio:**
  - Fortio Proxy — TLS-прокси и мультиплексирование gRPC/HTTP
  - Fortiotel — интеграция с OpenTelemetry
- **Встроенный язык сценариев**: `fortio script` (на базе [grol](https://grol.io/))

---

## Лицензия

Apache License 2.0

