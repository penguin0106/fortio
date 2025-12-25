// Package main demonstrates how to use Fortio with a custom logger.
package main

import (
	"os"

	"fortio.org/fortio/internal/bincommon"
	"fortio.org/fortio/internal/cli"
	"fortio.org/fortio/pkg/log"
)

func main() {
	// Запускаем Fortio с кастомным логгером
	os.Exit(cli.FortioMainWithConfig(&bincommon.FortioConfig{
		LoggerSetup: func() {
			// Создаём логгер с опциями
			logger := initLogger()
			logger.SetAsDefault()
		},
	}))
}

func initLogger() *log.Logger {
	logName := "fortio"
	logRelease := "0.0.0"
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
