// Пакет grol взаимодействует с движком скриптов GROL.
package grol

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"fortio.org/fortio/pkg/fgrpc"
	"fortio.org/fortio/pkg/fhttp"
	"fortio.org/fortio/pkg/periodic"
	"fortio.org/fortio/pkg/rapi"
	"fortio.org/fortio/pkg/tcprunner"
	"fortio.org/fortio/pkg/udprunner"
	"fortio.org/fortio/pkg/log"
	"grol.io/grol/eval"
	"grol.io/grol/extensions"
	"grol.io/grol/object"
	"grol.io/grol/repl"
)

// MapToStruct конвертирует grol map в структуру типа T через JSON roundtrip.
func MapToStruct[T any](t *T, omap object.Map) error {
	w := strings.Builder{}
	err := omap.JSON(&w)
	if err != nil {
		return err
	}
	err = json.Unmarshal([]byte(w.String()), t)
	if err != nil {
		return err
	}
	return nil
}

func createFortioGrolFunctions(state *eval.State, scriptInit string) error {
	fn := object.Extension{
		Name:    "fortio.load",
		MinArgs: 2,
		MaxArgs: 2,
		Help: "Запускает нагрузочный тест указанного типа (http, tcp, udp, grpc) с переданными параметрами map/json " +
			"(url, qps и т.д., добавьте \"save\":true для сохранения результата в файл)",
		ArgTypes:  []object.Type{object.STRING, object.MAP},
		Callback:  grolLoad,
		DontCache: true,
	}
	extensions.MustCreate(fn)
	fn.Name = "curl"
	fn.MinArgs = 1
	fn.MaxArgs = 2
	fn.Help = "fortio curl получает указанный url с опциональными параметрами"
	fn.Callback = grolCurl
	extensions.MustCreate(fn)
	// Короткий алиас для http нагрузочного теста; нельзя использовать "load" так как это встроенная функция grol для загрузки файлов.
	// Обратите внимание, мы не можем использовать eval.AddEvalResult() так как состояние уже создано.
	_, err := eval.EvalString(state, "func hload(options){fortio.load(\"http\", options)}", false)
	if err != nil {
		panic(err)
	}
	// Добавляем конвертацию из секунд в длительности int.
	_, err = eval.EvalString(state, "func duration(seconds){int(seconds * 1e9)}", false)
	if err != nil {
		panic(err)
	}
	// Вышеуказанные ошибки были бы багом, поэтому паника, а нижеследующее - ошибка пользователя.
	if scriptInit != "" {
		obj, err := eval.EvalString(state, scriptInit, false)
		if err != nil {
			return fmt.Errorf("для %q: %w", scriptInit, err)
		}
		log.Infof("Инициализация скрипта %q: %v", scriptInit, obj.Inspect())
	}
	return nil
}

func grolLoad(env any, _ string, args []object.Object) object.Object {
	s := env.(*eval.State)
	runType := args[0].(object.String).Value
	// в JSON и обратно в RunnerOptions
	omap := args[1].(object.Map)
	// Используем http как базовый/наиболее распространённый - в нём есть всё что нужно и мы можем перенести URL в
	// Destination для других типов.
	ro := fhttp.HTTPRunnerOptions{}
	err := MapToStruct(&ro, omap)
	rapi.CallHook(&ro.HTTPOptions, &ro.RunnerOptions)
	if err != nil {
		return s.Error(err)
	}
	// Восстанавливаем терминал в нормальный режим пока runner работает, чтобы ^C обрабатывался обычным кодом прерывания fortio.
	if s.Term != nil {
		s.Term.Suspend()
	}
	s.Context, s.Cancel = context.WithCancel(context.Background()) // без таймаута.
	log.LogVf("Запуск %s %#v", runType, ro)
	var res periodic.HasRunnerResult
	switch runType {
	case "http":
		res, err = fhttp.RunHTTPTest(&ro)
	case "tcp":
		tro := tcprunner.RunnerOptions{
			RunnerOptions: ro.RunnerOptions,
		}
		tro.Destination = ro.URL
		res, err = tcprunner.RunTCPTest(&tro)
	case "udp":
		uro := udprunner.RunnerOptions{
			RunnerOptions: ro.RunnerOptions,
		}
		uro.Destination = ro.URL
		res, err = udprunner.RunUDPTest(&uro)
	case "grpc":
		gro := fgrpc.GRPCRunnerOptions{}
		// повторно десериализуем так как grpc имеет уникальные опции.
		err = MapToStruct(&gro, omap)
		if err != nil {
			return s.Error(err)
		}
		if gro.Destination == "" {
			gro.Destination = ro.URL
		}
		res, err = fgrpc.RunGRPCTest(&gro)
	default:
		return s.Errorf("Тип запуска %q неожиданный", runType)
	}
	// Возвращаем в режим grol когда закончили. альтернатива - иметь ro.Out = s.Out и передать функцию отмены в runner.
	if s.Term != nil {
		s.Context, s.Cancel = s.Term.Resume(context.Background())
	}
	if err != nil {
		return s.Error(err)
	}
	jsonData, jerr := json.Marshal(res)
	if jerr != nil {
		return s.Error(jerr)
	}
	doSave, found := omap.Get(object.String{Value: "save"})
	if found && doSave == object.TRUE {
		fname := res.Result().ID + rapi.JSONExtension // третье место где мы это делаем или подобное...
		log.Infof("Сохранение %s", fname)
		err = os.WriteFile(fname, jsonData, 0o644) //nolint:gosec // мы хотим 644
		if err != nil {
			log.Errf("Не удалось сохранить %s: %v", fname, err)
			return s.Error(err)
		}
	}
	// Это по сути реализация "unjson".
	obj, err := eval.EvalString(s, string(jsonData), true)
	if err != nil {
		return s.Error(err)
	}
	return obj
}

func grolCurl(env any, _ string, args []object.Object) object.Object {
	s := env.(*eval.State)
	url := args[0].(object.String).Value
	httpOpts := fhttp.NewHTTPOptions(url)
	httpOpts.DisableFastClient = true
	httpOpts.FollowRedirects = true
	if len(args) > 1 {
		omap := args[1].(object.Map)
		err := MapToStruct(httpOpts, omap)
		if err != nil {
			return s.Error(err)
		}
	}
	var w bytes.Buffer
	httpOpts.DataWriter = &w
	rapi.CallHook(httpOpts, &periodic.RunnerOptions{})
	client, err := fhttp.NewClient(httpOpts)
	if err != nil {
		return s.Error(err)
	}
	code, _, _ := client.StreamFetch(context.Background())
	// должно быть предварительно отсортировано!
	return object.MakeQuad(
		object.String{Value: "body"}, object.String{Value: w.String()},
		object.String{Value: "code"}, object.Integer{Value: int64(code)},
	)
}

func ScriptMode(scriptInit string) int {
	// у нас уже есть либо 0, либо ровно 1 аргумент из парсинга флагов.
	interactive := len(flag.Args()) == 0
	options := repl.Options{
		ShowEval: true,
		// В интерактивном режиме состояние создаётся этой функцией, но есть Hook поэтому используем его
		// чтобы init скрипт мог также устанавливать состояние даже в интерактивном режиме.
		PreInput: func(s *eval.State) {
			err := createFortioGrolFunctions(s, scriptInit)
			if err != nil {
				log.Errf("Ошибка настройки начального окружения скрипта: %v", err)
			}
		},
	}
	// TODO: Перенести некоторые флаги из бинарника grol вместо жёстко закодированной "безопасной" конфигурации.
	c := extensions.Config{
		HasLoad:           true,
		HasSave:           interactive,
		UnrestrictedIOs:   interactive,
		LoadSaveEmptyOnly: false,
	}
	err := extensions.Init(&c)
	if err != nil {
		return log.FErrf("Ошибка инициализации расширений: %v", err)
	}
	if interactive {
		// Возможно перенести часть логики в пакет grol? (скопировано из main grol пока)
		homeDir, err := os.UserHomeDir()
		histFile := filepath.Join(homeDir, ".fortio_history")
		if err != nil {
			log.Warnf("Не удалось получить домашнюю директорию: %v", err)
			histFile = ""
		}
		options.HistoryFile = histFile
		options.MaxHistory = 99
		log.SetDefaultsForClientTools()
		log.Printf("Запуск интерактивного режима grol скрипта")
		return repl.Interactive(options)
	}
	scriptFile := flag.Arg(0)
	var reader io.Reader = os.Stdin
	if scriptFile != "-" {
		f, err := os.Open(scriptFile)
		if err != nil {
			return log.FErrf("%v", err)
		}
		defer f.Close()
		reader = f
	}
	s := eval.NewState()
	errs := repl.EvalAll(s, reader, os.Stdout, options)
	if len(errs) > 0 {
		return log.FErrf("Ошибки: %v", errs)
	}
	return 0
}
