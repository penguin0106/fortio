// histogram: читает значения из stdin и выводит гистограмму

package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"

	"fortio.org/fortio/pkg/stats"
	"fortio.org/fortio/pkg/log"
)

func main() {
	var (
		offsetFlag      = flag.Float64("offset", 0.0, "Смещение для данных")
		dividerFlag     = flag.Float64("divider", 1, "Делитель/масштаб для данных")
		percentilesFlag = flag.String("p", "50,75,99,99.9", "Список pXX для вычисления")
		jsonFlag        = flag.Bool("json", false, "Вывод в Json")
	)
	flag.Parse()
	h := stats.NewHistogram(*offsetFlag, *dividerFlag)
	percList, err := stats.ParsePercentiles(*percentilesFlag)
	if err != nil {
		log.Fatalf("Не удалось извлечь процентили из -p: %v", err)
	}

	scanner := bufio.NewScanner(os.Stdin)
	linenum := 1
	for scanner.Scan() {
		line := scanner.Text()
		v, err := strconv.ParseFloat(line, 64)
		if err != nil {
			log.Fatalf("Не удалось распарсить строку %d: %v", linenum, err)
		}
		h.Record(v)
		linenum++
	}
	if err := scanner.Err(); err != nil {
		log.Fatalf("Ошибка чтения стандартного ввода %v", err)
	}
	if *jsonFlag {
		b, err := json.MarshalIndent(h.Export().CalcPercentiles(percList), "", "  ")
		if err != nil {
			log.Fatalf("Не удалось создать Json: %v", err)
		}
		fmt.Print(string(b))
	} else {
		h.Print(os.Stdout, "Гистограмма", percList)
	}
}
