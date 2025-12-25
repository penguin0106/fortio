// Пакет bincommon содержит общий код и обработку флагов между исполняемыми файлами
// fortio (fortio_main.go) и fcurl (fcurl.go).
package bincommon

// Не добавляйте внешние зависимости - мы хотим сохранить fortio минимальным.

import (
	"context"
	"flag"
	"net/http"
	"os"
	"reflect"
	"strings"

	"fortio.org/dflag"
	"fortio.org/fortio/fhttp"
	"fortio.org/fortio/fnet"
	"fortio.org/fortio/periodic"
	"fortio.org/log"
	"fortio.org/safecast"
)

// FortioHook используется в fortio/cli и fortio/rapi для кастомизации запуска и внедрения,
// например, clienttrace и логгера доступа fortiotel.
type FortioHook func(*fhttp.HTTPOptions, *periodic.RunnerOptions)

var (
	compressionFlag = flag.Bool("compression", false, "Включить HTTP сжатие")
	keepAliveFlag   = flag.Bool("keepalive", true, "Поддерживать соединение (только для быстрого HTTP/1.1)")
	halfCloseFlag   = flag.Bool("halfclose", false,
		"Когда не keepalive, выполнять ли половинное закрытие соединения (только для быстрого http)")
	httpReqTimeoutFlag  = flag.Duration("timeout", fhttp.HTTPReqTimeOutDefaultValue, "Таймаут соединения и чтения (для HTTP)")
	stdClientFlag       = flag.Bool("stdclient", false, "Использовать более медленный стандартный клиент net/http (медленнее, но поддерживает h2/h2c)")
	http10Flag          = flag.Bool("http1.0", false, "Использовать HTTP/1.0 (вместо HTTP/1.1)")
	h2Flag              = flag.Bool("h2", false, "Попытаться использовать HTTP/2.0 / h2 (вместо HTTP/1.1) как для TLS, так и для h2c")
	httpsInsecureFlag   = flag.Bool("k", false, "Не проверять сертификаты в HTTPS/TLS/gRPC соединениях")
	httpsInsecureFlagL  = flag.Bool("https-insecure", false, "Длинная форма флага -k")
	resolve             = flag.String("resolve", "", "Разрешить имя хоста в этот `IP`")
	httpOpts            fhttp.HTTPOptions
	followRedirectsFlag = flag.Bool("L", false, "Следовать редиректам (подразумевает -std-client) - не использовать для нагрузочного тестирования")
	userCredentialsFlag = flag.String("user", "", "Учетные данные пользователя для базовой аутентификации (для HTTP). Формат: `user:password`")
	contentTypeFlag     = flag.String("content-type", "",
		"Устанавливает HTTP content type. Установка этого значения переключает метод запроса с GET на POST.")
	// PayloadSizeFlag - значение флага -payload-size.
	PayloadSizeFlag = flag.Int("payload-size", 0, "Дополнительный размер случайной полезной нагрузки, заменяет -payload когда > 0,"+
		" должен быть меньше -maxpayloadsizekb. Установка переключает HTTP на POST.")
	// PayloadFlag - значение флага -payload.
	PayloadFlag = flag.String("payload", "", "Строка полезной нагрузки для отправки")
	// PayloadFileFlag - значение флага -payload-file.
	PayloadFileFlag = flag.String("payload-file", "", "`Путь` к файлу для использования как полезная нагрузка (POST для HTTP), заменяет -payload когда установлен.")
	// PayloadStreamFlag для потоковой передачи полезной нагрузки из stdin (только curl).
	PayloadStreamFlag = flag.Bool("stream", false, "Потоковая передача полезной нагрузки из stdin (только для режима fortio curl)")
	// UnixDomainSocket для использования вместо обычного host:port.
	unixDomainSocketFlag = flag.String("unix-socket", "", "`Путь` к Unix domain socket для физического соединения")
	// CertFlag - флаг для пути к клиентскому сертификату.
	CertFlag = flag.String("cert", "", "`Путь` к файлу сертификата для клиентского или серверного TLS")
	// KeyFlag - флаг для пути к ключу для `cert`.
	KeyFlag = flag.String("key", "", "`Путь` к файлу ключа, соответствующего -cert")
	// CACertFlag - флаг для пути к кастомному CA для проверки серверных сертификатов.
	CACertFlag = flag.String("cacert", "",
		"`Путь` к файлу кастомного CA сертификата для TLS клиентских соединений, "+
			"если пусто, используется https:// префикс для стандартных интернет/системных CA")
	mTLS = flag.Bool("mtls", false, "Требовать клиентский сертификат, подписанный -cacert для клиентских соединений")
	// LogErrorsFlag определяет, логировать ли не-OK HTTP коды по мере их возникновения.
	LogErrorsFlag = flag.Bool("log-errors", true, "Логировать HTTP статусы не-2xx/418 по мере возникновения")
	// RunIDFlag - опциональный RunID для JSON результатов (и имени файла по умолчанию, если не 0).
	RunIDFlag = flag.Int64("runid", 0, "Опциональный RunID для добавления в JSON результат и авто-сохранение, для соответствия серверному режиму")
	// HelpFlag устанавливается в true, если пользователь запросил справку.
	warmupFlag = flag.Bool("sequential-warmup", false,
		"http(s) runner прогрев выполняется последовательно вместо параллельного. Восстанавливает поведение до 1.21")
	curlHeadersStdout = flag.Bool("curl-stdout-headers", false,
		"Восстановить поведение до 1.22, когда HTTP заголовки быстрого клиента выводятся в stdout в режиме curl. Теперь по умолчанию stderr.")
	// ConnectionReuseRange - динамический строковый флаг для установки диапазона переиспользования соединений.
	ConnectionReuseRange = dflag.Flag("connection-reuse", dflag.New("",
		"Диапазон `min:max` для максимального числа переиспользований соединения на поток, по умолчанию неограничен. "+
			"Например, 10:30 означает случайный выбор порога между 10 и 30 запросами.").
		WithValidator(ConnectionReuseRangeValidator(&httpOpts)))
	// NoReResolveFlag равен false, если мы хотим разрешать DNS имя для каждого нового соединения.
	NoReResolveFlag = flag.Bool("no-reresolve", false, "Сохранить начальное DNS разрешение и "+
		"не переразрешать при создании новых соединений (из-за ошибки или достижения лимита переиспользования)")
	MethodFlag = flag.String("X", "", "HTTP метод для использования вместо GET/POST в зависимости от payload/content-type")
)

// SharedMain - общая часть main из fortio_main и fcurl.
// Настраивает общие флаги, остальная обработка использования/аргументов/флагов
// теперь перенесена в пакеты [fortio.org/cli] и [fortio.org/scli].
func SharedMain() {
	flag.Func("H",
		"Дополнительный HTTP заголовок(и) или gRPC метаданные. Несколько пар `key:value` можно передать через несколько -H.",
		httpOpts.AddAndValidateExtraHeader)
	flag.IntVar(&fhttp.BufferSizeKb, "httpbufferkb", fhttp.BufferSizeKb,
		"Размер буфера (максимальный размер данных) для оптимизированного HTTP клиента в `килобайтах`")
	flag.BoolVar(&fhttp.CheckConnectionClosedHeader, "httpccch", fhttp.CheckConnectionClosedHeader,
		"Проверять заголовок Connection: Close")
	// FlagResolveIPType указывает какие типы IP разрешать.
	// С round-robin разрешением по умолчанию вы, вероятно, получите IPv6, который может не работать
	// если используете оба типа (`ip`). В частности, некоторые тестовые среды, такие как CI, имеют IPv6
	// для localhost, но не могут подключиться. Поэтому мы сделали по умолчанию только ip4.
	dflag.Flag("resolve-ip-type", fnet.FlagResolveIPType)
	// FlagResolveMethod решает какой метод использовать, когда для имени возвращается несколько IP.
	// По умолчанию предполагается, что все IP получены в первом вызове и выполняется round-robin по ним.
	// first просто берёт первый ответ, rr чередует каждый ответ.
	dflag.Flag("dns-method", fnet.FlagResolveMethod)
	dflag.Flag("echo-server-default-params", fhttp.DefaultEchoServerParams)
	dflag.FlagBool("proxy-all-headers", fhttp.Fetch2CopiesAllHeader)
	dflag.Flag("server-idle-timeout", fhttp.ServerIdleTimeout)
	// MaxDelay - максимальная задержка для ответов echoserver.
	// Это динамический флаг со значением по умолчанию 1.5s для тестирования таймаута 1s в envoy.
	dflag.Flag("max-echo-delay", fhttp.MaxDelay)
	// вызовите [scli.ServerMain()] для завершения настройки.
}

// FetchURL получает содержимое URL и завершается с кодом 1 при ошибке.
// Общая часть между fortio_main и fcurl.
func FetchURL(o *fhttp.HTTPOptions) {
	// keepAlive мог бы быть false при одном запросе, но это помогает
	// отлаживать HTTP клиент при одном запросе с использованием флагов
	o.DataWriter = os.Stdout
	client, _ := fhttp.NewClient(o)
	// большая ловушка: nil client не является nil interface value (!)
	if client == nil || reflect.ValueOf(client).IsNil() {
		os.Exit(1) // ошибка уже залогирована
	}
	var code int
	var dataLen int64
	var header uint
	if client.HasBuffer() {
		// Быстрый клиент
		var data []byte
		var headerI int
		code, data, headerI = client.Fetch(context.Background()) //nolint:staticcheck // сохраняем обратную совместимость Fetch пока
		dataLen = safecast.MustConv[int64](len(data))
		header = safecast.MustConv[uint](headerI)
		if *curlHeadersStdout {
			os.Stdout.Write(data)
		} else {
			os.Stderr.Write(data[:header])
			os.Stdout.Write(data[header:])
		}
	} else {
		code, dataLen, header = client.StreamFetch(context.Background())
	}
	log.LogVf("Результат Fetch код %d, длина данных %d, длина заголовка %d", code, dataLen, header)
	if code != http.StatusOK {
		log.Errf("Статус ошибки %d", code)
		os.Exit(1)
	}
}

// TLSInsecure возвращает true, если был передан -k или -https-insecure.
func TLSInsecure() bool {
	TLSInsecure := *httpsInsecureFlag || *httpsInsecureFlagL
	if TLSInsecure {
		log.Infof("TLS сертификаты не будут проверяться, по запросу флага")
	} else {
		log.LogVf("Будут проверяться TLS сертификаты, используйте -k / -https-insecure для отключения")
	}
	return TLSInsecure
}

// ConnectionReuseRangeValidator возвращает функцию валидатора, которая проверяет корректность
// диапазона переиспользования соединений и устанавливает его в httpOpts.
func ConnectionReuseRangeValidator(httpOpts *fhttp.HTTPOptions) func(string) error {
	return func(value string) error {
		return httpOpts.ValidateAndSetConnectionReuseRange(value)
	}
}

// SharedHTTPOptions - код переноса флагов->httpoptions, общий между
// fortio_main и fcurl. Также устанавливает fhttp.DefaultHTTPOptions.
func SharedHTTPOptions() *fhttp.HTTPOptions {
	url := strings.TrimLeft(flag.Arg(0), " \t\r\n")
	httpOpts.URL = url
	httpOpts.HTTP10 = *http10Flag
	httpOpts.H2 = *h2Flag
	httpOpts.DisableFastClient = *stdClientFlag
	httpOpts.DisableKeepAlive = !*keepAliveFlag
	httpOpts.AllowHalfClose = *halfCloseFlag
	httpOpts.Compression = *compressionFlag
	httpOpts.HTTPReqTimeOut = *httpReqTimeoutFlag
	httpOpts.Insecure = TLSInsecure()
	httpOpts.Resolve = *resolve
	httpOpts.UserCredentials = *userCredentialsFlag
	if len(*contentTypeFlag) > 0 {
		// устанавливаем content-type из флага только если флаг не пустой, так как он может прийти из -H content-type:...
		httpOpts.ContentType = *contentTypeFlag
	}
	if *PayloadStreamFlag {
		httpOpts.PayloadReader = os.Stdin
	} else {
		// Возвращает nil при ошибке чтения файла, пустой но не nil slice если полезная нагрузка не запрошена.
		httpOpts.Payload = fnet.GeneratePayload(*PayloadFileFlag, *PayloadSizeFlag, *PayloadFlag)
		if httpOpts.Payload == nil {
			// Ошибка уже залогирована
			os.Exit(1)
		}
	}
	httpOpts.UnixDomainSocket = *unixDomainSocketFlag
	if *followRedirectsFlag {
		httpOpts.FollowRedirects = true
		httpOpts.DisableFastClient = true
	}
	httpOpts.CACert = *CACertFlag
	httpOpts.Cert = *CertFlag
	httpOpts.Key = *KeyFlag
	httpOpts.MTLS = *mTLS
	httpOpts.LogErrors = *LogErrorsFlag
	httpOpts.SequentialWarmup = *warmupFlag
	httpOpts.NoResolveEachConn = *NoReResolveFlag
	httpOpts.MethodOverride = *MethodFlag
	fhttp.DefaultHTTPOptions = &httpOpts
	return &httpOpts
}
