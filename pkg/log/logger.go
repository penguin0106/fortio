// Package log предоставляет логгер для Fortio на базе slog.
// Позволяет использовать кастомный slog.Handler или стандартный.
package log

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	stdlog "log"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
)

// Level определяет уровень логирования (совместимость с fortio.org/log).
type Level int8

// Уровни логирования (совместимость с fortio.org/log).
const (
	Debug    Level = iota // 0
	Verbose               // 1
	Info                  // 2
	Warning               // 3
	Error                 // 4
	Critical              // 5
	Fatal                 // 6
	NoLevel               // 7
)

// slog уровни
const (
	LevelDebug = slog.LevelDebug
	LevelInfo  = slog.LevelInfo
	LevelWarn  = slog.LevelWarn
	LevelError = slog.LevelError
)

var (
	// defaultLogger - глобальный логгер по умолчанию
	defaultLogger *Logger
	loggerMu      sync.RWMutex
	// defaultLevel - уровень логирования по умолчанию
	defaultLevel = new(slog.LevelVar)
)

// Logger обёртка над slog.Logger с дополнительными метаданными.
type Logger struct {
	*slog.Logger
	name        string
	release     string
	environment string
	level       Level
	output      io.Writer
}

// Option функция для настройки Logger.
type Option func(*Logger)

// WithName устанавливает имя логгера.
func WithName(name string) Option {
	return func(l *Logger) {
		l.name = name
	}
}

// WithRelease устанавливает версию релиза.
func WithRelease(release string) Option {
	return func(l *Logger) {
		l.release = release
	}
}

// WithEnvironment устанавливает окружение (local, dev, prod).
func WithEnvironment(env string) Option {
	return func(l *Logger) {
		l.environment = env
	}
}

// WithLevel устанавливает уровень логирования.
func WithLevel(level Level) Option {
	return func(l *Logger) {
		l.level = level
	}
}

// WithLevelString устанавливает уровень логирования из строки.
func WithLevelString(levelStr string) Option {
	return func(l *Logger) {
		l.level = ParseLevel(levelStr)
	}
}

// WithOutput устанавливает writer для вывода.
func WithOutput(w io.Writer) Option {
	return func(l *Logger) {
		l.output = w
	}
}

// WithHandler устанавливает кастомный slog.Handler.
func WithHandler(h slog.Handler) Option {
	return func(l *Logger) {
		l.Logger = slog.New(h)
	}
}

// ParseLevel парсит строку уровня логирования.
func ParseLevel(s string) Level {
	switch strings.ToUpper(s) {
	case "DEBUG":
		return Debug
	case "VERBOSE":
		return Verbose
	case "INFO":
		return Info
	case "WARN", "WARNING":
		return Warning
	case "ERROR":
		return Error
	case "CRITICAL":
		return Critical
	case "FATAL":
		return Fatal
	default:
		return Info
	}
}

// New создаёт новый Logger с опциями.
//
// Пример:
//
//	logger := log.New(
//	    log.WithName("myapp"),
//	    log.WithRelease("1.0.0"),
//	    log.WithEnvironment("production"),
//	    log.WithLevelString("DEBUG"),
//	)
func New(opts ...Option) *Logger {
	l := &Logger{
		name:        "fortio",
		release:     "0.0.0",
		environment: "local",
		level:       Info,
		output:      os.Stderr,
	}

	// Применяем опции
	for _, opt := range opts {
		opt(l)
	}

	// Создаём slog.Logger если не был установлен через WithHandler
	if l.Logger == nil {
		levelVar := new(slog.LevelVar)
		levelVar.Set(levelToSlog(l.level))

		handler := slog.NewJSONHandler(l.output, &slog.HandlerOptions{
			Level: levelVar,
		})

		// Добавляем базовые атрибуты
		l.Logger = slog.New(handler).With(
			slog.String("name", l.name),
			slog.String("release", l.release),
			slog.String("environment", l.environment),
		)
	}

	return l
}

// SetAsDefault устанавливает этот логгер как глобальный.
func (l *Logger) SetAsDefault() {
	loggerMu.Lock()
	defer loggerMu.Unlock()
	defaultLogger = l
	defaultLevel.Set(levelToSlog(l.level))
	slog.SetDefault(l.Logger)
}

// Name возвращает имя логгера.
func (l *Logger) Name() string {
	return l.name
}

// Release возвращает версию релиза.
func (l *Logger) Release() string {
	return l.release
}

// Environment возвращает окружение.
func (l *Logger) Environment() string {
	return l.environment
}

func init() {
	// Инициализируем логгер по умолчанию
	defaultLevel.Set(LevelInfo)
	defaultLogger = New(
		WithName("fortio"),
		WithLevel(Info),
	)
}

// SetDefault устанавливает глобальный slog.Logger.
func SetDefault(l *slog.Logger) {
	loggerMu.Lock()
	defer loggerMu.Unlock()
	defaultLogger = &Logger{Logger: l, level: Info}
	slog.SetDefault(l)
}

// SetDefaultLogger устанавливает глобальный Logger.
func SetDefaultLogger(l *Logger) {
	loggerMu.Lock()
	defer loggerMu.Unlock()
	defaultLogger = l
	defaultLevel.Set(levelToSlog(l.level))
	slog.SetDefault(l.Logger)
}

// Default возвращает глобальный slog.Logger.
func Default() *slog.Logger {
	loggerMu.RLock()
	defer loggerMu.RUnlock()
	if defaultLogger == nil {
		return slog.Default()
	}
	return defaultLogger.Logger
}

// DefaultLogger возвращает глобальный Logger.
func DefaultLogger() *Logger {
	loggerMu.RLock()
	defer loggerMu.RUnlock()
	return defaultLogger
}

// SetLevel устанавливает уровень логирования.
func SetLevel(level Level) {
	defaultLevel.Set(levelToSlog(level))
}

// SetLogLevel алиас для SetLevel (совместимость с fortio.org/log).
func SetLogLevel(level Level) {
	SetLevel(level)
}

// SetLogLevelQuiet устанавливает уровень без логирования (совместимость).
func SetLogLevelQuiet(level Level) {
	defaultLevel.Set(levelToSlog(level))
}

// GetLevel возвращает текущий уровень логирования.
func GetLevel() Level {
	return slogToLevel(defaultLevel.Level())
}

// GetLogLevel алиас для GetLevel (совместимость с fortio.org/log).
func GetLogLevel() Level {
	return GetLevel()
}

// levelToSlog конвертирует наш Level в slog.Level.
func levelToSlog(level Level) slog.Level {
	switch level {
	case Debug, Verbose:
		return slog.LevelDebug
	case Info, NoLevel:
		return slog.LevelInfo
	case Warning:
		return slog.LevelWarn
	case Error, Critical, Fatal:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// slogToLevel конвертирует slog.Level в наш Level.
func slogToLevel(level slog.Level) Level {
	switch {
	case level <= slog.LevelDebug:
		return Debug
	case level <= slog.LevelInfo:
		return Info
	case level <= slog.LevelWarn:
		return Warning
	default:
		return Error
	}
}

// SetOutput устанавливает вывод для логгера (создаёт новый handler).
func SetOutput(w io.Writer) {
	loggerMu.Lock()
	defer loggerMu.Unlock()
	slogger := slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: defaultLevel,
	}))
	if defaultLogger != nil {
		defaultLogger.Logger = slogger
		defaultLogger.output = w
	} else {
		defaultLogger = &Logger{Logger: slogger, output: w, level: Info}
	}
	slog.SetDefault(slogger)
}

// SetHandler устанавливает кастомный handler для логгера.
func SetHandler(h slog.Handler) {
	loggerMu.Lock()
	defer loggerMu.Unlock()
	slogger := slog.New(h)
	if defaultLogger != nil {
		defaultLogger.Logger = slogger
	} else {
		defaultLogger = &Logger{Logger: slogger, level: Info}
	}
	slog.SetDefault(slogger)
}

// WithContext возвращает логгер с контекстом.
func WithContext(ctx context.Context) *slog.Logger {
	return Default()
}

// With возвращает логгер с дополнительными атрибутами.
func With(args ...any) *slog.Logger {
	return Default().With(args...)
}

// WithGroup возвращает логгер с группой.
func WithGroup(name string) *slog.Logger {
	return Default().WithGroup(name)
}

// --- Функции логирования (совместимость с fortio.org/log) ---

// Debugf логирует сообщение на уровне Debug с форматированием.
func Debugf(format string, args ...any) {
	Default().Debug(fmt.Sprintf(format, args...))
}

// Infof логирует сообщение на уровне Info с форматированием.
func Infof(format string, args ...any) {
	Default().Info(fmt.Sprintf(format, args...))
}

// Warnf логирует сообщение на уровне Warn с форматированием.
func Warnf(format string, args ...any) {
	Default().Warn(fmt.Sprintf(format, args...))
}

// Errf логирует сообщение на уровне Error с форматированием.
func Errf(format string, args ...any) {
	Default().Error(fmt.Sprintf(format, args...))
}

// Fatalf логирует сообщение на уровне Error и завершает программу.
func Fatalf(format string, args ...any) {
	Default().Error(fmt.Sprintf(format, args...))
	os.Exit(1)
}

// LogVf логирует на уровне Debug (verbose).
func LogVf(format string, args ...any) {
	Default().Debug(fmt.Sprintf(format, args...))
}

// Critf логирует критическую ошибку (уровень Error).
func Critf(format string, args ...any) {
	Default().Error(fmt.Sprintf(format, args...))
}

// Printf логирует на уровне Info (совместимость с log.Printf).
func Printf(format string, args ...any) {
	Default().Info(fmt.Sprintf(format, args...))
}

// Log проверяет, включён ли указанный уровень логирования.
// Возвращает true если уровень включён (совместимость с fortio.org/log).
// Используется как: if log.Log(log.Warning) { ... }
func Log(level Level) bool {
	return GetLevel() <= level
}

// Logf логирует на указанном уровне с форматированием.
func Logf(level Level, format string, args ...any) {
	if !Log(level) {
		return
	}
	msg := fmt.Sprintf(format, args...)
	switch level {
	case Debug, Verbose:
		Default().Debug(msg)
	case Info, NoLevel:
		Default().Info(msg)
	case Warning:
		Default().Warn(msg)
	case Error, Critical, Fatal:
		Default().Error(msg)
	default:
		Default().Info(msg)
	}
}

// Attr создаёт атрибут для structured logging (совместимость с fortio.org/log).
func Attr(key string, value any) slog.Attr {
	return slog.Any(key, value)
}

// S логирует structured сообщение с атрибутами на указанном уровне.
// Пример: log.S(log.Info, "msg", log.Attr("key", "value"))
// Совместимость с fortio.org/log.
func S(level Level, msg string, attrs ...slog.Attr) {
	if !Log(level) {
		return
	}
	switch level {
	case Debug, Verbose:
		Default().LogAttrs(context.Background(), slog.LevelDebug, msg, attrs...)
	case Info, NoLevel:
		Default().LogAttrs(context.Background(), slog.LevelInfo, msg, attrs...)
	case Warning:
		Default().LogAttrs(context.Background(), slog.LevelWarn, msg, attrs...)
	case Error, Critical, Fatal:
		Default().LogAttrs(context.Background(), slog.LevelError, msg, attrs...)
	default:
		Default().LogAttrs(context.Background(), slog.LevelInfo, msg, attrs...)
	}
}

// Slog возвращает slog.Logger для direct access.
func Slog() *slog.Logger {
	return Default()
}

// --- Функции с явным контекстом ---

// DebugContext логирует с контекстом на уровне Debug.
func DebugContext(ctx context.Context, msg string, args ...any) {
	Default().DebugContext(ctx, msg, args...)
}

// InfoContext логирует с контекстом на уровне Info.
func InfoContext(ctx context.Context, msg string, args ...any) {
	Default().InfoContext(ctx, msg, args...)
}

// WarnContext логирует с контекстом на уровне Warn.
func WarnContext(ctx context.Context, msg string, args ...any) {
	Default().WarnContext(ctx, msg, args...)
}

// ErrorContext логирует с контекстом на уровне Error.
func ErrorContext(ctx context.Context, msg string, args ...any) {
	Default().ErrorContext(ctx, msg, args...)
}

// --- Проверки уровня ---

// IsDebug возвращает true если уровень Debug включён.
func IsDebug() bool {
	return defaultLevel.Level() <= LevelDebug
}

// IsInfo возвращает true если уровень Info включён.
func IsInfo() bool {
	return defaultLevel.Level() <= LevelInfo
}

// LogDebug проверяет, включён ли уровень Debug (совместимость с fortio.org/log).
func LogDebug() bool {
	return Log(Debug)
}

// LogVerbose проверяет, включён ли уровень Verbose (совместимость с fortio.org/log).
func LogVerbose() bool {
	return Log(Verbose)
}

// --- Хелперы для атрибутов ---

// String создаёт строковый атрибут.
func String(key, value string) slog.Attr {
	return slog.String(key, value)
}

// Str алиас для String (совместимость с fortio.org/log).
func Str(key, value string) slog.Attr {
	return slog.String(key, value)
}

// Int создаёт целочисленный атрибут.
func Int(key string, value int) slog.Attr {
	return slog.Int(key, value)
}

// Int64 создаёт int64 атрибут.
func Int64(key string, value int64) slog.Attr {
	return slog.Int64(key, value)
}

// Float64 создаёт float64 атрибут.
func Float64(key string, value float64) slog.Attr {
	return slog.Float64(key, value)
}

// Bool создаёт булев атрибут.
func Bool(key string, value bool) slog.Attr {
	return slog.Bool(key, value)
}

// Any создаёт атрибут любого типа.
func Any(key string, value any) slog.Attr {
	return slog.Any(key, value)
}

// Err создаёт атрибут ошибки.
func Err(err error) slog.Attr {
	return slog.Any("error", err)
}

// Group создаёт группу атрибутов.
func Group(key string, args ...any) slog.Attr {
	return slog.Group(key, args...)
}

// --- HTTP хелперы (совместимость с fortio.org/log) ---

// FErrf логирует ошибку и возвращает код ошибки 1.
// Используется для return log.FErrf(...) паттерна.
func FErrf(format string, args ...any) int {
	Default().Error(fmt.Sprintf(format, args...))
	return 1
}

// NewStdLogger создаёт стандартный логгер для http.Server.
func NewStdLogger(prefix string, level Level) *stdlog.Logger {
	return stdlog.New(&logWriter{level: level, prefix: prefix}, "", 0)
}

// logWriter реализует io.Writer для std log.
type logWriter struct {
	level  Level
	prefix string
}

func (w *logWriter) Write(p []byte) (n int, err error) {
	msg := string(p)
	if w.prefix != "" {
		msg = w.prefix + ": " + msg
	}
	Logf(w.level, "%s", msg)
	return len(p), nil
}

// LogAndCall оборачивает http.Handler для логирования запросов.
func LogAndCall(name string, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		Infof("%s: %s %s from %s", name, r.Method, r.URL.Path, r.RemoteAddr)
		handler(w, r)
	}
}

// LogRequest логирует HTTP запрос.
func LogRequest(r *http.Request, msg string) {
	Infof("%s: %s %s from %s", msg, r.Method, r.URL.Path, r.RemoteAddr)
}

// TLSInfo возвращает информацию о TLS соединении из HTTP запроса.
func TLSInfo(r *http.Request) string {
	if r == nil || r.TLS == nil {
		return ""
	}
	return fmt.Sprintf(" TLS %s", tls.VersionName(r.TLS.Version))
}

// SetColorMode устанавливает цветной режим (no-op для совместимости).
func SetColorMode() {}

// SetDefaultsForClientTools устанавливает настройки для CLI (no-op для совместимости).
func SetDefaultsForClientTools() {}

// SetFlags устанавливает флаги логгера (no-op для совместимости).
func SetFlags(flags int) {}

// Config для совместимости с fortio.org/log.
var Config = &LogConfig{
	LogFileAndLine: false,
	LogPrefix:      "",
	JSON:           true,
	ConsoleColor:   false,
	GoroutineID:    false,
}

// LogConfig конфигурация логгера (совместимость с fortio.org/log).
type LogConfig struct {
	LogFileAndLine bool
	LogPrefix      string
	JSON           bool
	ConsoleColor   bool
	GoroutineID    bool
	NoTimestamp    bool
}
