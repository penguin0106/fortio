package main

// Не добавляйте внешние зависимости - мы хотим сохранить fortio минимальным.

import (
	"os"

	"fortio.org/cli"
	"fortio.org/fortio/internal/bincommon"
	"fortio.org/fortio/pkg/log"
)

func Main() int {
	cli.ProgramName = "Φορτίο fortio-curl"
	cli.ArgsHelp = "url"
	cli.MinArgs = 1
	bincommon.SharedMain()
	cli.Main()
	o := bincommon.SharedHTTPOptions()
	log.Debugf("Запуск curl с %+v", o)
	bincommon.FetchURL(o)
	return 0
}

func main() {
	os.Exit(Main())
}
