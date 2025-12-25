package stats // import "fortio.org/fortio/pkg/stats"

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"fortio.org/fortio/pkg/log"
)

// Counter is a type whose instances record values
// and calculate stats (count, average, min, max, and stddev).
// Counter — это тип, экземпляры которого записывают значения
// и вычисляют статистику (количество, среднее, минимум, максимум и стандартное отклонение).
type Counter struct {
	Count        int64
	Min          float64
	Max          float64
	Sum          float64
	sumOfSquares float64
}

// Record records a data point.
func (c *Counter) Record(v float64) {
	c.RecordN(v, 1)
}

// RecordN efficiently records the same value N times.
// RecordN эффективно записывает одно и то же значение N раз.
func (c *Counter) RecordN(v float64, n int) {
	isFirst := (c.Count == 0)
	c.Count += int64(n)
	switch {
	case isFirst:
		c.Min = v
		c.Max = v
	case v < c.Min:
		c.Min = v
	case v > c.Max:
		c.Max = v
	}
	s := v * float64(n)
	c.Sum += s
	c.sumOfSquares += (s * s)
}

// Avg returns the average.
// Avg возвращает среднее значение.
func (c *Counter) Avg() float64 {
	if c.Count == 0 {
		return 0.
	}
	return c.Sum / float64(c.Count)
}

// StdDev returns the standard deviation.
// StdDev возвращает стандартное отклонение.
func (c *Counter) StdDev() float64 {
	if c.Count == 0 {
		return 0.
	}
	fC := float64(c.Count)
	sigma := (c.sumOfSquares - c.Sum*c.Sum/fC) / fC
	// should never happen but it does
	if sigma < 0 {
		log.Warnf("Unexpected negative sigma for %+v: %g", c, sigma)
		return 0
	}
	return math.Sqrt(sigma)
}

// Print prints stats.
// Print выводит статистику.
func (c *Counter) Print(out io.Writer, msg string) {
	_, _ = fmt.Fprintf(out, "%s : count %d avg %.8g +/- %.4g min %g max %g sum %.9g\n",
		msg, c.Count, c.Avg(), c.StdDev(), c.Min, c.Max, c.Sum)
}

// Log outputs the stats to the logger.
// Log выводит статистику в логгер.
func (c *Counter) Log(msg string) {
	log.Infof("%s : count %d avg %.8g +/- %.4g min %g max %g sum %.9g",
		msg, c.Count, c.Avg(), c.StdDev(), c.Min, c.Max, c.Sum)
}

// Reset clears the counter to reset it to original 'no data' state.
// Reset очищает счетчик, сбрасывая его в исходное состояние 'без данных'.
func (c *Counter) Reset() {
	var empty Counter
	*c = empty
}

// Transfer merges the data from src into this Counter and clears src.
// Transfer объединяет данные из src в этот Counter и очищает src.
func (c *Counter) Transfer(src *Counter) {
	if src.Count == 0 {
		return // nothing to do / ничего делать не нужно
	}
	if c.Count == 0 {
		*c = *src // copy everything at once
		src.Reset()
		return
	}
	c.Count += src.Count
	if src.Min < c.Min {
		c.Min = src.Min
	}
	if src.Max > c.Max {
		c.Max = src.Max
	}
	c.Sum += src.Sum
	c.sumOfSquares += src.sumOfSquares
	src.Reset()
}

// Histogram - written in go with inspiration from https://github.com/facebook/wdt/blob/master/util/Stats.h
// Histogram - написано на Go с вдохновением от https://github.com/facebook/wdt/blob/master/util/Stats.h

// The intervals are [prev, current] so for "90" (previous is 80) the values in that bucket are >80 and <=90
// that way a cumulative % up to that bucket means X% of the data <= 90 (or 100-X% > 90), works well for max too
// There are 2 special buckets - the first one is from min to and including 0,
// one after the last for value > last and up to max.
// Интервалы [prev, current], так что для "90" (предыдущий 80) значения в этом бакете >80 и <=90
// таким образом кумулятивный % до этого бакета означает X% данных <= 90 (или 100-X% > 90), хорошо работает и для max
// Есть 2 специальных бакета - первый от min до включительно 0,
// один после последнего для значений > last и до max.
var (
	histogramBucketValues = []int32{
		0, 1, 2, 3, 4, 5, 6,
		7, 8, 9, 10, 11, // initially increment buckets by 1, my amp goes to 11 !
		12, 14, 16, 18, 20, // then by 2
		25, 30, 35, 40, 45, 50, // then by 5
		60, 70, 80, 90, 100, // then by 10
		120, 140, 160, 180, 200, // line3 *10
		250, 300, 350, 400, 450, 500, // line4 *10
		600, 700, 800, 900, 1000, // line5 *10
		2000, 3000, 4000, 5000, 7500, 10000, // another order of magnitude coarsly covered
		20000, 30000, 40000, 50000, 75000, 100000, // ditto, the end
	}
	numValues  = len(histogramBucketValues)
	numBuckets = numValues + 1 // 1 special first bucket is <= 0; and 1 extra last bucket is > 100000
	firstValue = float64(histogramBucketValues[0])
	lastValue  = float64(histogramBucketValues[numValues-1])
	val2Bucket []int // ends at 1000. Remaining values will not be received in constant time.

	maxArrayValue      = int32(1000) // Last value looked up as O(1) array, the rest is linear search
	maxArrayValueIndex = -1          // Index of maxArrayValue
)

// Histogram extends Counter and adds a histogram.
// Must be created using NewHistogram or anotherHistogram.Clone()
// and not directly.
// Histogram расширяет Counter и добавляет гистограмму.
// Должен создаваться с помощью NewHistogram или anotherHistogram.Clone(),
// а не напрямую.
type Histogram struct {
	Counter
	Offset  float64 // offset applied to data before fitting into buckets
	Divider float64 // divider applied to data before fitting into buckets
	// Don't access directly (outside of this package):
	Hdata []int32 // numValues buckets (one more than values, for last one)
}

// For export of the data:
// Для экспорта данных:

// Interval is a range from start to end.
// Interval are left closed, open right expect the last one which includes Max.
// i.e., [Start, End] with the next one being [PrevEnd, NextEnd].
// Interval — это диапазон от start до end.
// Интервалы закрыты слева, открыты справа, кроме последнего, который включает Max.
// т.е. [Start, End], следующий будет [PrevEnd, NextEnd].
type Interval struct {
	Start float64
	End   float64
}

// Bucket is the data for 1 bucket: an Interval and the occurrence Count for
// that interval.
// Bucket — это данные для 1 бакета: Interval и количество вхождений Count для этого интервала.
type Bucket struct {
	Interval
	Percent float64 // Cumulative percentile
	Count   int64   // How many in this bucket
}

// Percentile value for the percentile.
// Percentile — значение для перцентиля.
type Percentile struct {
	Percentile float64 // For this Percentile
	Value      float64 // value at that Percentile
}

// HistogramData is the exported Histogram data, a sorted list of intervals
// covering [Min, Max]. Pure data, so Counter for instance is flattened.
// HistogramData — это экспортированные данные гистограммы, отсортированный список интервалов,
// покрывающих [Min, Max]. Чистые данные, поэтому Counter, например, сглажен.
type HistogramData struct {
	Count       int64
	Min         float64
	Max         float64
	Sum         float64
	Avg         float64
	StdDev      float64
	Data        []Bucket
	Percentiles []Percentile `json:"Percentiles,omitempty"`
}

// NewHistogram creates a new histogram (sets up the buckets).
// Divider value can not be zero, otherwise returns zero.
// NewHistogram создает новую гистограмму (настраивает бакеты).
// Значение Divider не может быть нулем, иначе возвращается nil.
func NewHistogram(offset float64, divider float64) *Histogram {
	if divider == 0 {
		return nil
	}
	h := Histogram{
		Offset:  offset,
		Divider: divider,
		Hdata:   make([]int32, numBuckets),
	}
	return &h
}

// Val2Bucket values are kept in two different structure
// val2Bucket allows you reach between 0 and 1000 in constant time.
// Val2Bucket значения хранятся в двух разных структурах
// val2Bucket позволяет получить доступ к значениям от 0 до 1000 за константное время.
//
//nolint:gochecknoinits // we need to init these / нам нужно их инициализировать.
func init() {
	val2Bucket = make([]int, maxArrayValue)
	maxArrayValueIndex = -1
	for i, value := range histogramBucketValues {
		if value == maxArrayValue {
			maxArrayValueIndex = i
			break
		}
	}
	if maxArrayValueIndex == -1 {
		log.Fatalf("Bug boundary maxArrayValue=%d not found in bucket list %v", maxArrayValue, histogramBucketValues)
	}
	idx := 0
	for i := range maxArrayValue {
		if i >= histogramBucketValues[idx] {
			idx++
		}
		val2Bucket[i] = idx
	}
	// coding bug detection (aka impossible if it works once) until 1000
	if idx != maxArrayValueIndex {
		log.Fatalf("Bug in creating histogram index idx %d vs index %d up to %d", idx, int(maxArrayValue), maxArrayValue)
	}
}

// lookUpIdx looks for scaledValue's index in histogramBucketValues
// TODO: change linear time to O(log(N)) with binary search.
// lookUpIdx ищет индекс scaledValue в histogramBucketValues
// TODO: изменить линейное время на O(log(N)) с бинарным поиском.
func lookUpIdx(scaledValue int) int {
	scaledValue32 := int32(scaledValue) //nolint:gosec // we limit ourselves to 32 bits counts.
	if scaledValue32 < maxArrayValue {  // constant
		return val2Bucket[scaledValue]
	}
	for i := maxArrayValueIndex; i < numValues; i++ {
		if histogramBucketValues[i] > scaledValue32 {
			return i
		}
	}
	log.Fatalf("never reached/bug")
	return 0
}

// Record records a data point.
func (h *Histogram) Record(v float64) {
	h.RecordN(v, 1)
}

// RecordN efficiently records a data point N times.
func (h *Histogram) RecordN(v float64, n int) {
	h.Counter.RecordN(v, n)
	h.record(v, n)
}

// Records v value to count times.
// Записывает значение v count раз.
func (h *Histogram) record(v float64, count int) {
	// Scaled value to bucketize - we used to subtract epsilon because the interval
	// is open to the left ] start, end ] so when exactly on start it has
	// to fall on the previous bucket: which is more correctly done using
	// math.Ceil()-1 but that doesn't work... so back to epsilon distance.
	scaledVal := (v - h.Offset) / h.Divider
	var idx int
	switch {
	case scaledVal <= firstValue:
		idx = 0
	case scaledVal > lastValue:
		idx = numBuckets - 1 // last bucket is for > last value
	default:
		// else we look it up (with the open interval adjustment)
		svInt := int(scaledVal)
		delta := scaledVal - float64(svInt)
		if delta < 1e-12 {
			svInt--
		}
		log.Debugf("v %f -> scaledVal %.17f ceil %f delta %g - svInt %d", v, scaledVal, math.Ceil(scaledVal), delta, svInt)
		idx = lookUpIdx(svInt)
	}
	h.Hdata[idx] += int32(count) //nolint:gosec // we limit ourselves to 32 bits counts.
}

// CalcPercentile returns the value for an input percentile
// e.g., for 90. as input returns an estimate of the original value threshold
// where 90.0% of the data is below said threshold.
// with 3 data points 10, 20, 30; p0-p33.33 == 10, p 66.666 = 20, p100 = 30
// p33.333 - p66.666 = linear between 10 and 20; so p50 = 15
// TODO: consider spreading the count of the bucket evenly from start to end
// so the % grows by at least to 1/N on start of range, and for last range
// when start == end we should get to that % faster.
// CalcPercentile возвращает значение для входного перцентиля
// например, для входа 90. возвращает оценку исходного порогового значения,
// ниже которого находится 90.0% данных.
// с 3 точками данных 10, 20, 30; p0-p33.33 == 10, p 66.666 = 20, p100 = 30
// p33.333 - p66.666 = линейно между 10 и 20; так что p50 = 15
// TODO: рассмотреть равномерное распределение количества бакета от начала до конца
// так чтобы % рос как минимум на 1/N в начале диапазона, и для последнего диапазона
// когда start == end мы должны достигать этого % быстрее.
func (e *HistogramData) CalcPercentile(percentile float64) float64 {
	if len(e.Data) == 0 {
		log.Errf("Unexpected call to CalcPercentile(%g) with no data", percentile)
		return 0
	}
	if percentile >= 100 {
		return e.Max
	}
	// We assume Min is at least a single point so at least covers 1/Count %
	pp := 100. / float64(e.Count) // previous percentile
	if percentile <= pp {
		return e.Min
	}
	for i := range e.Data {
		cur := &e.Data[i]
		if percentile <= cur.Percent {
			return cur.Start + (percentile-pp)/(cur.Percent-pp)*(cur.End-cur.Start)
		}
		pp = cur.Percent
	}
	return e.Max // not reached
}

// Export translate the internal representation of the histogram data in
// an externally usable one. Calculates the request Percentiles.
// Export преобразует внутреннее представление данных гистограммы
// во внешне используемое. Вычисляет запрошенные перцентили.
func (h *Histogram) Export() *HistogramData {
	var res HistogramData
	res.Count = h.Counter.Count
	res.Min = h.Counter.Min
	res.Max = h.Counter.Max
	res.Sum = h.Counter.Sum
	res.Avg = h.Counter.Avg()
	res.StdDev = h.Counter.StdDev()
	multiplier := h.Divider
	offset := h.Offset
	// calculate the last bucket index
	lastIdx := -1
	for i := numBuckets - 1; i >= 0; i-- {
		if h.Hdata[i] > 0 {
			lastIdx = i
			break
		}
	}
	if lastIdx == -1 {
		return &res
	}

	// previous bucket value:
	prev := histogramBucketValues[0]
	var total int64
	ctrTotal := float64(h.Count)
	// export the data of each bucket of the histogram
	for i := 0; i <= lastIdx; i++ {
		if h.Hdata[i] == 0 {
			// empty bucket: skip it, but update prev which is needed for next iteration
			if i < numValues {
				prev = histogramBucketValues[i]
			}
			continue
		}
		var b Bucket
		total += int64(h.Hdata[i])
		if len(res.Data) == 0 {
			// First entry, start is min
			b.Start = h.Min
		} else {
			b.Start = multiplier*float64(prev) + offset
		}
		b.Percent = 100. * float64(total) / ctrTotal
		if i < numValues {
			cur := histogramBucketValues[i]
			b.End = multiplier*float64(cur) + offset
			prev = cur
		} else {
			// Last Entry
			b.End = h.Max
		}
		b.Count = int64(h.Hdata[i])
		res.Data = append(res.Data, b)
	}
	res.Data[len(res.Data)-1].End = h.Max
	return &res
}

// CalcPercentiles calculates the requested percentile and adds them to the
// HistogramData. Potential TODO: sort or assume sorting and calculate all
// the percentiles in 1 pass (greater and greater values).
// CalcPercentiles вычисляет запрошенные перцентили и добавляет их в
// HistogramData. Потенциальный TODO: отсортировать или предполагать сортировку и вычислить все
// перцентили за 1 проход (все большие и большие значения).
func (e *HistogramData) CalcPercentiles(percentiles []float64) *HistogramData {
	if e.Count == 0 {
		return e
	}
	for _, p := range percentiles {
		e.Percentiles = append(e.Percentiles, Percentile{p, e.CalcPercentile(p)})
	}
	return e
}

// Print dumps the histogram (and counter) to the provided writer.
// Also calculates the percentile.
// Print выводит гистограмму (и счетчик) в предоставленный writer.
// Также вычисляет перцентиль.
func (e *HistogramData) Print(out io.Writer, msg string) {
	if len(e.Data) == 0 {
		_, _ = fmt.Fprintf(out, "%s : no data\n", msg)
		return
	}
	// the base counterpart:
	_, _ = fmt.Fprintf(out, "%s : count %d avg %.8g +/- %.4g min %g max %g sum %.9g\n",
		msg, e.Count, e.Avg, e.StdDev, e.Min, e.Max, e.Sum)
	_, _ = fmt.Fprintln(out, "# range, mid point, percentile, count")
	sep := ">="
	for i := range e.Data {
		b := &e.Data[i]
		if i > 0 {
			sep = ">" // last interval is inclusive (of max value)
		}
		_, _ = fmt.Fprintf(out, "%s %.6g <= %.6g , %.6g , %.2f, %d\n", sep, b.Start, b.End, (b.Start+b.End)/2., b.Percent, b.Count)
	}
	// print the information of target percentiles
	for _, p := range e.Percentiles {
		_, _ = fmt.Fprintf(out, "# target %g%% %.6g\n", p.Percentile, p.Value)
	}
}

// Print dumps the histogram (and counter) to the provided writer.
// Also calculates the percentiles. Use Export() once and Print if you
// are going to need the Export results too.
// Print выводит гистограмму (и счетчик) в предоставленный writer.
// Также вычисляет перцентили. Используйте Export() один раз и Print, если
// вам также понадобятся результаты Export.
func (h *Histogram) Print(out io.Writer, msg string, percentiles []float64) {
	h.Export().CalcPercentiles(percentiles).Print(out, msg)
}

// Log Logs the histogram to the counter.
// Log логирует гистограмму в счетчик.
func (h *Histogram) Log(msg string, percentiles []float64) {
	var b bytes.Buffer
	w := bufio.NewWriter(&b)
	h.Print(w, msg, percentiles)
	_ = w.Flush()
	log.Infof("%s", b.Bytes())
}

// Reset clears the data. Reset it to NewHistogram state.
// Reset очищает данные. Сбрасывает в состояние NewHistogram.
func (h *Histogram) Reset() {
	h.Counter.Reset()
	// Leave Offset and Divider alone / Оставляем Offset и Divider без изменений
	for i := 0; i < len(h.Hdata); i++ {
		h.Hdata[i] = 0
	}
}

// Clone returns a copy of the histogram.
// Clone возвращает копию гистограммы.
func (h *Histogram) Clone() *Histogram {
	hCopy := NewHistogram(h.Offset, h.Divider)
	hCopy.CopyFrom(h)
	return hCopy
}

// CopyFrom sets the content of this object to a copy of the src.
// CopyFrom устанавливает содержимое этого объекта в копию src.
func (h *Histogram) CopyFrom(src *Histogram) {
	h.Counter = src.Counter
	h.copyHDataFrom(src)
}

// copyHDataFrom appends histogram data values to this object from the src.
// Src histogram data values will be appended according to this object's
// offset and divider.
// copyHDataFrom добавляет значения данных гистограммы в этот объект из src.
// Значения данных гистограммы src будут добавлены в соответствии с
// offset и divider этого объекта.
func (h *Histogram) copyHDataFrom(src *Histogram) {
	if h.Divider == src.Divider && h.Offset == src.Offset {
		for i := 0; i < len(h.Hdata); i++ {
			h.Hdata[i] += src.Hdata[i]
		}
		return
	}
	hData := src.Export()
	for i := range hData.Data {
		data := hData.Data[i]
		h.record((data.Start+data.End)/2, int(data.Count))
	}
}

// Merge two different histogram with different scale parameters
// Lowest offset and highest divider value will be selected on new Histogram as scale parameters.
// Merge объединяет две разные гистограммы с разными параметрами масштаба
// Наименьший offset и наибольший divider будут выбраны для новой гистограммы как параметры масштаба.
func Merge(h1 *Histogram, h2 *Histogram) *Histogram {
	divider := h1.Divider
	offset := h1.Offset
	if h2.Divider > h1.Divider {
		divider = h2.Divider
	}
	if h2.Offset < h1.Offset {
		offset = h2.Offset
	}
	newH := NewHistogram(offset, divider)
	newH.Transfer(h1)
	newH.Transfer(h2)
	return newH
}

// Transfer merges the data from src into this Histogram and clears src.
// Transfer объединяет данные из src в эту гистограмму и очищает src.
func (h *Histogram) Transfer(src *Histogram) {
	if src.Count == 0 {
		return
	}
	if h.Count == 0 {
		h.CopyFrom(src)
		src.Reset()
		return
	}
	h.copyHDataFrom(src)
	h.Counter.Transfer(&src.Counter)
	src.Reset()
}

// ParsePercentiles extracts the percentiles from string (flag).
// ParsePercentiles извлекает перцентили из строки (флага).
func ParsePercentiles(percentiles string) ([]float64, error) {
	percs := strings.Split(percentiles, ",") // will make a size 1 array for empty input!
	res := make([]float64, 0, len(percs))
	for _, pStr := range percs {
		pStr = strings.TrimSpace(pStr)
		if len(pStr) == 0 {
			continue
		}
		p, err := strconv.ParseFloat(pStr, 64)
		if err != nil {
			return res, err
		}
		if p <= 0 || p >= 100 {
			return res, fmt.Errorf("percentile %g must be > 0 and < 100", p)
		}
		res = append(res, p)
	}
	if len(res) == 0 {
		return res, errors.New("list can't be empty")
	}
	log.LogVf("Will use %v for percentiles", res)
	return res, nil
}

// RoundToDigits rounds the input to digits number of digits after decimal point.
// Note this incorrectly rounds the last digit of negative numbers.
// RoundToDigits округляет ввод до digits знаков после запятой.
// Обратите внимание, что это неправильно округляет последнюю цифру отрицательных чисел.
func RoundToDigits(v float64, digits int) float64 {
	p := math.Pow(10, float64(digits))
	return math.Floor(v*p+0.5) / p
}

// Round rounds to 4 digits after the decimal point.
// Round округляет до 4 знаков после запятой.
func Round(v float64) float64 {
	return RoundToDigits(v, 4)
}

// Occurrence is a type that stores the occurrences of the keys.
// Could be directly an alias for map[string]int but keeping the
// outer struct for parity with Counter and Histogram and to keep
// 1.38's API.
// Occurrence — это тип, который хранит вхождения ключей.
// Мог бы быть напрямую псевдонимом для map[string]int, но сохраняем
// внешнюю структуру для паритета с Counter и Histogram и для сохранения
// API версии 1.38.
type Occurrence struct {
	m map[string]int
}

// NewOccurrence create a new occurrence (map).
// NewOccurrence создает новый экземпляр occurrence (карту).
func NewOccurrence() *Occurrence {
	return &Occurrence{m: make(map[string]int)}
}

// Record records a new occurrence of the key.
// Record записывает новое вхождение ключа.
func (o *Occurrence) Record(key string) {
	o.m[key]++
}

// AggregateAndToString aggregates the data from the object into the passed in totals map
// and returns a string suitable for printing usage counts per key of the incoming object.
// AggregateAndToString агрегирует данные из объекта в переданную карту totals
// и возвращает строку, подходящую для вывода счетчиков использования по ключу входящего объекта.
func (o *Occurrence) AggregateAndToString(totals map[string]int) string {
	var sb strings.Builder
	sb.WriteString("[")

	first := true
	onlyOne := (len(o.m) == 1)

	for k, v := range o.m {
		totals[k] += v
		if onlyOne {
			// Special case for single entry in the map, no [] form
			// and the count is omitted (already printed in runner IP count case).
			return k
		}
		if first {
			first = false
		} else {
			sb.WriteString(", ")
		}
		sb.WriteString(k)
		sb.WriteString(fmt.Sprintf(" (%d)", v))
	}
	sb.WriteString("]")
	return sb.String()
}
