package main

import (
	"os"

	"fortio.org/fortio/cli"
)

// Содержимое этого файла перемещено в cli/fortio_main.go, чтобы его можно было
// переиспользовать в вариантах fortio, таких как fortiotel (fortio с opentelemetry)

func main() {
	os.Exit(cli.FortioMain(nil /* хук не нужен */))
}

// То же самое, что и выше, но для тестов testscript/txtar.

func Main() int {
	return cli.FortioMain(nil /* хук не нужен */)
}
