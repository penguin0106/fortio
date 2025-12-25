// Package jrpc is an opinionated JSON-RPC / REST style library. Facilitates web JSON calls,
// using generics to serialize/deserialize any type.
// Пакет jrpc — это библиотека в стиле JSON-RPC / REST. Облегчает веб JSON вызовы,
// используя дженерики для сериализации/десериализации любого типа.
package jrpc // import "fortio.org/fortio/internal/jrpc"

// This package is a true self-contained library, that doesn't rely on our logger nor other packages
// in fortio/ outside of version/ (which now also doesn't rely on logger or any other package).
// Naming is hard, we have Call, Send, Get, Fetch and FetchBytes pretty much all meaning retrieving data
// from a URL with the variants depending on whether we have something to serialize and if it's bytes
// or struct based in and out. Additionally, *URL() variants are for when no additional headers or options
// are needed, and the URL is just a plain string. If golang supported multiple signatures it would be a single
// method name instead of 8.
// Этот пакет является самодостаточной библиотекой, которая не зависит от нашего логгера или других пакетов
// в fortio/ за пределами version/ (который теперь также не зависит от логгера или любого другого пакета).
// Именование затруднено, у нас есть Call, Send, Get, Fetch и FetchBytes, которые по сути означают получение данных
// из URL с вариантами, зависящими от того, есть ли у нас что-то для сериализации и являются ли это байтами
// или структурой на входе и выходе. Кроме того, варианты *URL() предназначены для случаев, когда дополнительные заголовки или опции
// не нужны, а URL — просто строка. Если бы golang поддерживал множественные сигнатуры, это было бы одно
// имя метода вместо 8.

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"time"

	"fortio.org/fortio/pkg/version"
	"fortio.org/sets"
)

// Client side and common code.
// Клиентская сторона и общий код.

const (
	UserAgentHeader = "User-Agent"
)

// Default timeout for Call.
// Таймаут по умолчанию для Call.
var timeout = 60 * time.Second

// UserAgent is the User-Agent header used by client calls (also used in fhttp/).
// UserAgent — это заголовок User-Agent, используемый клиентскими вызовами (также используется в fhttp/).
var UserAgent = "fortio.org/fortio-" + version.Short()

// SetCallTimeout changes the timeout for further Call calls, returns
// the previous value (default in 60s). Value is used when a timeout
// isn't passed in the options. Note this is not thread safe,
// use Destination.Timeout for changing values outside of main/single
// thread.
// SetCallTimeout изменяет таймаут для последующих вызовов Call, возвращает
// предыдущее значение (по умолчанию 60с). Значение используется, когда таймаут
// не передан в опциях. Обратите внимание, что это не потокобезопасно,
// используйте Destination.Timeout для изменения значений вне main/одиночного потока.
func SetCallTimeout(t time.Duration) time.Duration {
	previous := timeout
	timeout = t
	return previous
}

// FetchError is a custom error type that preserves HTTP result code if obtained.
// FetchError — это пользовательский тип ошибки, который сохраняет код результата HTTP, если он получен.
type FetchError struct {
	Message string
	// HTTP status code if present, -1 for other errors.
	// Код состояния HTTP, если присутствует, -1 для других ошибок.
	Code int
	// Original (wrapped) error if any
	// Исходная (обернутая) ошибка, если есть
	Err error
	// Original reply payload if any
	// Исходная полезная нагрузка ответа, если есть
	Bytes []byte
}

// Destination is the URL and optional additional headers.
// Depending on your needs consider also https://pkg.go.dev/fortio.org/multicurl/mc#MultiCurl
// and its configuration https://pkg.go.dev/fortio.org/multicurl/mc#Config object.
// Destination — это URL и необязательные дополнительные заголовки.
// В зависимости от ваших потребностей рассмотрите также https://pkg.go.dev/fortio.org/multicurl/mc#MultiCurl
// и его конфигурацию https://pkg.go.dev/fortio.org/multicurl/mc#Config объект.
type Destination struct {
	URL string
	// Default is nil, which means no additional headers.
	// По умолчанию nil, что означает отсутствие дополнительных заголовков.
	Headers *http.Header
	// Default is 0 which means use global timeout.
	// По умолчанию 0, что означает использование глобального таймаута.
	Timeout time.Duration
	// Default is "" which will use POST if there is a payload and GET otherwise.
	// По умолчанию "", что будет использовать POST, если есть полезная нагрузка, и GET в противном случае.
	Method string
	// Context or will be context.Background() if not set.
	// Context или будет context.Background(), если не установлен.
	//nolint:containedctx // backward compatibility and keeping the many APIs simple as it's optional
	// https://go.dev/blog/context-and-structs
	Context context.Context
	// ClientTrace to use if set.
	// ClientTrace для использования, если установлен.
	ClientTrace *httptrace.ClientTrace
	// TLSConfig to use if set. This is ignored if HTTPClient is set.
	// Otherwise that setting this implies a new http.Client each call where this is set.
	// TLSConfig для использования, если установлен. Это игнорируется, если HTTPClient установлен.
	// В противном случае установка этого подразумевает новый http.Client для каждого вызова, где это установлено.
	TLSConfig *tls.Config
	// Ok codes. If nil (default) then 200, 201, 202 are ok.
	// Коды успеха. Если nil (по умолчанию), то 200, 201, 202 считаются успешными.
	OkCodes sets.Set[int]
	// Only use this if all the options above are not enough. Defaults to http.DefaultClient.
	// Используйте это только если всех вышеуказанных опций недостаточно. По умолчанию http.DefaultClient.
	Client *http.Client
}

func (d *Destination) GetContext() context.Context {
	if d.Context != nil {
		return d.Context
	}
	return context.Background()
}

func (fe *FetchError) Error() string {
	return fmt.Sprintf("%s, code %d: %v (raw reply: %s)", fe.Message, fe.Code, fe.Err, DebugSummary(fe.Bytes, 256))
}

func (fe *FetchError) Unwrap() error {
	return fe.Err
}

// Call calls the URL endpoint, POSTing a serialized as JSON optional payload
// (pass nil for a GET HTTP request) and returns the result, deserializing
// JSON into type Q. T can be inferred so we declare Response Q first.
// Call вызывает URL-эндпоинт, отправляя POST с сериализованной в JSON необязательной полезной нагрузкой
// (передайте nil для GET HTTP запроса) и возвращает результат, десериализуя
// JSON в тип Q. T может быть выведен, поэтому мы сначала объявляем Response Q.
func Call[Q any, T any](url *Destination, payload *T) (*Q, error) {
	var bytes []byte
	var err error
	if payload != nil {
		bytes, err = json.Marshal(payload)
		if err != nil {
			return nil, err
		}
	}
	return Fetch[Q](url, bytes)
}

// CallURL is Call without any options/non default headers, timeout etc and just the URL.
// CallURL — это Call без каких-либо опций/нестандартных заголовков, таймаута и т.д., только URL.
func CallURL[Q any, T any](url string, payload *T) (*Q, error) {
	return Call[Q](NewDestination(url), payload)
}

// Get fetches and deserializes the JSON returned by the Destination into a Q struct.
// Used when there is no JSON payload to send. Note that Get can be a different http
// method than GET, for instance if url.Method is set to "POST".
// Get получает и десериализует JSON, возвращенный Destination, в структуру Q.
// Используется, когда нет JSON полезной нагрузки для отправки. Обратите внимание, что Get может быть другим HTTP
// методом, кроме GET, например, если url.Method установлен в "POST".
func Get[Q any](url *Destination) (*Q, error) {
	return Fetch[Q](url, []byte{})
}

// GetArray fetches and deserializes the JSON returned by the Destination into a slice of
// Q struct (ie the response is a JSON array).
// GetArray получает и десериализует JSON, возвращенный Destination, в срез
// структур Q (т.е. ответ — это JSON массив).
func GetArray[Q any](url *Destination) ([]Q, error) {
	slicePtr, err := Fetch[[]Q](url, []byte{})
	if slicePtr == nil {
		return nil, err
	}
	return *slicePtr, err
}

// GetURL is Get without additional options (default timeout and headers).
// GetURL — это Get без дополнительных опций (таймаут и заголовки по умолчанию).
func GetURL[Q any](url string) (*Q, error) {
	return Get[Q](NewDestination(url))
}

// Serialize serializes the object as json.
// Serialize сериализует объект как json.
func Serialize(obj any) ([]byte, error) {
	return json.Marshal(obj)
}

// Deserialize deserializes JSON as a new object of desired type.
// Deserialize десериализует JSON в новый объект нужного типа.
func Deserialize[Q any](bytes []byte) (*Q, error) {
	var result Q
	if len(bytes) == 0 {
		// Allow empty body to be deserialized as empty object.
		// Разрешить пустое тело для десериализации как пустой объект.
		return &result, nil
	}
	err := json.Unmarshal(bytes, &result)
	return &result, err // Will return zero object, not nil upon error
}

// Fetch is for cases where the payload is already serialized (or empty
// but call Get() when empty for clarity).
// Note that if you're looking for the []byte version instead of this
// generics version, it's now called FetchBytes().
// Fetch для случаев, когда полезная нагрузка уже сериализована (или пустая,
// но вызывайте Get() когда пустая для ясности).
// Обратите внимание, что если вы ищете версию []byte вместо этой
// версии с дженериками, теперь она называется FetchBytes().
func Fetch[Q any](url *Destination, bytes []byte) (*Q, error) {
	code, bytes, err := Send(url, bytes) // returns -1 on other errors
	if err != nil {
		return nil, err
	}
	var ok bool
	if url.OkCodes != nil {
		ok = url.OkCodes.Has(code)
	} else {
		// Default is 200, 201, 202 are ok
		// По умолчанию 200, 201, 202 считаются успешными
		ok = (code >= http.StatusOK && code <= http.StatusAccepted)
	}
	result, err := Deserialize[Q](bytes)
	if err != nil {
		if ok {
			return nil, err
		}
		return nil, &FetchError{"non ok http result and deserialization error", code, err, bytes}
	}
	if !ok {
		// can still be "ok" for some callers, they can use the result object as it deserialized as expected.
		// все еще может быть "ok" для некоторых вызывающих, они могут использовать результирующий объект, так как он десериализован как ожидалось.
		return result, &FetchError{"non ok http result", code, nil, bytes}
	}
	return result, nil
}

// SetHeaderIfMissing utility function to not overwrite nor append to existing headers.
// SetHeaderIfMissing — вспомогательная функция, чтобы не перезаписывать и не добавлять к существующим заголовкам.
func SetHeaderIfMissing(headers http.Header, name, value string) {
	if headers.Get(name) != "" {
		return
	}
	headers.Set(name, value)
}

// Send fetches the result from URL and sends optional payload as a POST, GET if missing.
// Returns the HTTP status code (if no other error before then, -1 if there are errors),
// the bytes from the reply and error if any.
// Send получает результат из URL и отправляет необязательную полезную нагрузку как POST, GET если отсутствует.
// Возвращает код состояния HTTP (если не было других ошибок до этого, -1 если есть ошибки),
// байты из ответа и ошибку, если есть.
func Send(dest *Destination, jsonPayload []byte) (int, []byte, error) {
	curTimeout := dest.Timeout
	if curTimeout == 0 {
		curTimeout = timeout
	}
	ctx, cancel := context.WithTimeout(dest.GetContext(), curTimeout)
	defer cancel()
	var req *http.Request
	var err error
	var res []byte
	method := dest.Method
	if len(jsonPayload) > 0 {
		if method == "" {
			method = http.MethodPost
		}
		req, err = http.NewRequestWithContext(ctx, method, dest.URL, bytes.NewReader(jsonPayload))
	} else {
		if method == "" {
			method = http.MethodGet
		}
		req, err = http.NewRequestWithContext(ctx, method, dest.URL, nil)
	}
	if dest.ClientTrace != nil {
		req = req.WithContext(httptrace.WithClientTrace(req.Context(), dest.ClientTrace))
	}
	if err != nil {
		return -1, res, err
	}
	if dest.Headers != nil {
		req.Header = dest.Headers.Clone()
	}
	if len(jsonPayload) > 0 {
		SetHeaderIfMissing(req.Header, "Content-Type", "application/json; charset=utf-8")
	}
	SetHeaderIfMissing(req.Header, "Accept", "application/json")
	SetHeaderIfMissing(req.Header, UserAgentHeader, UserAgent)
	var client *http.Client
	switch {
	case dest.Client != nil:
		client = dest.Client
	case dest.TLSConfig != nil:
		transport := http.DefaultTransport.(*http.Transport).Clone() // Let it crash/panic if somehow DefaultTransport is not a Transport
		transport.TLSClientConfig = dest.TLSConfig
		client = &http.Client{Transport: transport}
	default:
		client = http.DefaultClient
	}
	var resp *http.Response
	resp, err = client.Do(req)
	if err != nil {
		return -1, res, err
	}
	res, err = io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp.StatusCode, res, err
}

// NewDestination returns a Destination object set for the given url
// (and default/nil replacement headers and default global timeout).
// NewDestination возвращает объект Destination, настроенный для данного url
// (с заголовками по умолчанию/nil и глобальным таймаутом по умолчанию).
func NewDestination(url string) *Destination {
	return &Destination{URL: url}
}

// FetchURL is Send without a payload and no additional options (default timeout and headers).
// Technically this should be called FetchBytesURL().
// FetchURL — это Send без полезной нагрузки и без дополнительных опций (таймаут и заголовки по умолчанию).
// Технически это должно называться FetchBytesURL().
func FetchURL(url string) (int, []byte, error) {
	return Send(NewDestination(url), []byte{})
}

// FetchBytes is Send without a payload (so will be a GET request).
// Used to be called Fetch() but we needed that shorter name to
// simplify the former CallWithPayload function name.
// FetchBytes — это Send без полезной нагрузки (так что будет GET запрос).
// Раньше назывался Fetch(), но нам понадобилось это более короткое имя,
// чтобы упростить прежнее имя функции CallWithPayload.
func FetchBytes(url *Destination) (int, []byte, error) {
	return Send(url, []byte{})
}

// EscapeBytes returns printable string. Same as %q format without the
// surrounding/extra "".
// EscapeBytes возвращает печатную строку. То же, что формат %q без
// окружающих/дополнительных "".
func EscapeBytes(buf []byte) string {
	e := fmt.Sprintf("%q", buf)
	return e[1 : len(e)-1]
}

// DebugSummary returns a string with the size and escaped first max/2 and
// last max/2 bytes of a buffer (or the whole escaped buffer if small enough).
// DebugSummary возвращает строку с размером и экранированными первыми max/2 и
// последними max/2 байтами буфера (или весь экранированный буфер, если достаточно мал).
func DebugSummary(buf []byte, maxV int) string {
	l := len(buf)
	if l <= maxV+3 { // no point in shortening to add ... if we could return those 3
		return EscapeBytes(buf)
	}
	maxV /= 2
	return fmt.Sprintf("%d: %s...%s", l, EscapeBytes(buf[:maxV]), EscapeBytes(buf[l-maxV:]))
}
