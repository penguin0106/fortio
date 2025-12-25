// Изначально адаптировано из istio/proxy/test/backend/echo с исправлениями
// обработки ошибок и параллелизма, сделано максимально легковесным
// (без стандартного вывода по умолчанию)

package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"fortio.org/fortio/fhttp"
	"fortio.org/fortio/version"
)

var (
	port      = flag.String("port", "8080", "http порт по умолчанию, можно указать порт или адрес:порт")
	debugPath = flag.String("debug-path", "/debug", "путь для debug url, пустое значение отключает эту часть")
	certFlag  = flag.String("cert", "", "`Путь` к файлу сертификата для клиентского или серверного TLS")
	keyFlag   = flag.String("key", "", "`Путь` к файлу ключа, соответствующего -cert")
)

func main() {
	flag.Parse()
	if len(os.Args) >= 2 && strings.Contains(os.Args[1], "version") {
		fmt.Println(version.Full())
		os.Exit(0)
	}
	if _, addr := fhttp.ServeTLS(*port, *debugPath, &fhttp.TLSOptions{Cert: *certFlag, Key: *keyFlag}); addr == nil {
		os.Exit(1) // ошибка уже залогирована
	}
	select {}
}
