// Пакет version для fortio содержит информацию о версии и сборке.
// Переиспользуемая библиотечная часть и примеры перемещены в [fortio.org/version].
package version // import "fortio.org/fortio/version"
import (
	"fortio.org/version"
)

var (
	// Следующие переменные (пере)вычисляются в init().
	shortVersion = "dev"
	longVersion  = "unknown long"
	fullVersion  = "unknown full"
)

// Short возвращает 3-значную короткую строку версии fortio Major.Minor.Patch
// соответствует git тегу проекта (без ведущего v) или "dev" когда
// не собрано из тега / не `go install fortio.org/fortio@latest`
// version.Short() - общая версия проекта (используется также для версионирования json вывода).
func Short() string {
	return shortVersion
}

// Long возвращает длинную версию fortio и информацию о сборке.
// Формат: "X.Y.X hash go-version processor os".
func Long() string {
	return longVersion
}

// Full возвращает Long версию + всю информацию BuildInfo времени выполнения, т.е.
// все зависимые модули и их версии и хеши.
func Full() string {
	return fullVersion
}

// Это "прошивает" версию fortio. Нужно получить "правильные" версии.
// в зависимости от того, являемся ли мы модулем или main.
func init() { //nolint:gochecknoinits // нам действительно нужен init для этого
	shortVersion, longVersion, fullVersion = version.FromBuildInfoPath("fortio.org/fortio")
}
