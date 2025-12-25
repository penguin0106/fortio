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
			initLogger().SetAsDefault()
		},
	}))
}

// initLogger создаёт логгер с настройками из ENV переменных.
func initLogger() *log.Logger {
	logName := "fortio"
	logRelease := "0.0.0"
	logEnvironment := "local"
	logLevel := "INFO"

	if v := os.Getenv("LOGGER_NAME"); v != "" {
		logName = v
	}
	if v := os.Getenv("RELEASE"); v != "" {
		logRelease = v
	}
	if v := os.Getenv("ENVIRONMENT"); v != "" {
		logEnvironment = v
	}
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		logLevel = v
	}

	return log.New(
		log.WithName(logName),
		log.WithRelease(logRelease),
		log.WithEnvironment(logEnvironment),
		log.WithLevelString(logLevel),
	)
}

// Main то же самое, что и выше, но для тестов testscript/txtar.
func Main() int {
	return cli.FortioMainWithConfig(&bincommon.FortioConfig{
		LoggerSetup: func() {
			initLogger().SetAsDefault()
		},
	})
}
