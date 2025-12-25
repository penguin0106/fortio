package main

import (
	"os"
	"testing"

	"fortio.org/testscript"
)

func TestMain(m *testing.M) {
	// Запускает тесты cli_test.txtar (https://github.com/fortio/testscript#testscript).
	os.Exit(testscript.RunMain(m, map[string]func() int{
		"fortio": Main,
	}))
}

func TestFortioCli(t *testing.T) {
	testscript.Run(t, testscript.Params{Dir: "./"})
}
