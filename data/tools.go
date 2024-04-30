package data

import (
	"archive/zip"
	"encoding/csv"
	"fmt"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/utils"
	"github.com/banbox/banexg"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"go.uber.org/zap"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	ConcurNum = 5 // 并发处理的数量
	// 下面几个在Build1mWithTicks中使用，从tick构建1m K线
	symKLines = make(map[string][]*banexg.Kline) // 当前年的1m数据，键是合约ID
	klineLock sync.Mutex                         //symKlines的并发读写锁
)

type FuncConvert func(inPath string, file *zip.File, writer *zip.Writer) *errs.Error

type FuncReadZipItem func(inPath string, file *zip.File) *errs.Error

type FuncTickBar func(inPath string, row []string) (string, int64, float64, float64)

func zipConvert(inPath, outPath, suffix string, convert FuncConvert, pBar *utils.PrgBar) *errs.Error {
	r, err := zip.OpenReader(inPath)
	if err != nil {
		pBar.Add(core.StepTotal)
		return errs.New(errs.CodeIOReadFail, err)
	}
	defer r.Close()
	if err = utils.EnsureDir(filepath.Dir(outPath), 0755); err != nil {
		pBar.Add(core.StepTotal)
		return errs.New(errs.CodeIOWriteFail, err)
	}
	out, err := os.Create(outPath)
	if err != nil {
		pBar.Add(core.StepTotal)
		return errs.New(errs.CodeIOWriteFail, err)
	}
	defer out.Close()
	zipWriter := zip.NewWriter(out)
	defer zipWriter.Close()
	bar := pBar.NewJob(len(r.File))
	defer bar.Done()
	for _, f := range r.File {
		bar.Add(1)
		if f.FileInfo().IsDir() || !strings.HasSuffix(f.Name, suffix) {
			continue
		}
		err2 := convert(inPath, f, zipWriter)
		if err2 != nil {
			return err2
		}
	}
	return nil
}

func FindPathNames(inPath, suffix string) ([]string, *errs.Error) {
	inPath = filepath.Clean(inPath)
	info, err_ := os.Stat(inPath)
	if err_ != nil {
		return nil, errs.New(errs.CodeIOReadFail, err_)
	}
	var result []string
	if info.IsDir() {
		err_ = filepath.WalkDir(inPath, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			name := d.Name()
			if strings.HasSuffix(name, suffix) {
				path = filepath.Clean(path)
				subName := strings.Replace(path, inPath, "", -1)
				result = append(result, subName[1:])
			}
			return nil
		})
		if err_ != nil {
			return nil, errs.New(errs.CodeIOReadFail, err_)
		}
	} else if strings.HasSuffix(inPath, suffix) {
		result = append(result, info.Name())
	}
	return result, nil
}

func convertFiles(inPath, outPath, srcSuffix string, makeOutPath func(string, string) string, convert FuncConvert) *errs.Error {
	names, err := FindPathNames(inPath, ".zip")
	if err != nil {
		return err
	}
	pBar := utils.NewPrgBar(len(names)*core.StepTotal, "")
	defer pBar.Close()
	return utils.ParallelRun(names, ConcurNum, func(name string) *errs.Error {
		fileOutPath := makeOutPath(outPath, name)
		_, err_ := os.Stat(fileOutPath)
		if err_ == nil {
			// 已存在，跳过
			pBar.Add(core.StepTotal)
			return nil
		}
		fileInPath := filepath.Join(inPath, name)
		return zipConvert(fileInPath, fileOutPath, srcSuffix, convert, pBar)
	})
}

func isRawContract(name string) bool {
	parts := strings.Split(name, "&")
	if len(parts) > 1 {
		// 跳过价差数据
		return false
	}
	// 检查是否以价差前缀开始
	parts = strings.Split(name, " ")
	prefix := strings.ToUpper(parts[0])
	if prefix == "SPD" || prefix == "SPC" || prefix == "SP" || prefix == "IPS" {
		return false
	}
	// 检查是否最后一个字母是数字
	parts = strings.Split(name, ".")
	cleanName := parts[0]
	if len(parts) > 1 {
		cleanName = parts[len(parts)-2]
	}
	if cleanName == "README" || strings.HasSuffix(cleanName, "efp") {
		// 跳过期货转现货rfp
		return false
	}
	if strings.HasSuffix(cleanName, "TAS") {
		// 以结算价交易
		return true
	}
	if cleanName == "IMCI" {
		return true
	}
	lastChar := cleanName[len(cleanName)-1]
	if lastChar < '0' || lastChar > '9' {
		// 文件名不是以数字结尾的，不是合约数据，跳过
		log.Info("skip non tick", zap.String("name", name))
		return false
	}
	return true
}

func RunFormatTick(args *config.CmdArgs) *errs.Error {
	if args.InPath == "" {
		return errs.NewMsg(errs.CodeParamRequired, "--in is required")
	}
	if args.OutPath == "" {
		return errs.NewMsg(errs.CodeParamRequired, "--out is required")
	}
	layout := "20060102 15:04:05"
	loc, err_ := time.LoadLocation("Asia/Shanghai")
	if err_ != nil {
		return errs.New(errs.CodeRunTime, err_)
	}
	handleEntry := func(inPath string, file *zip.File, out *zip.Writer) *errs.Error {
		if !isRawContract(file.Name) {
			return nil
		}
		fReader, err_ := file.Open()
		if err_ != nil {
			return errs.New(errs.CodeIOReadFail, err_)
		}
		rows, err_ := csv.NewReader(fReader).ReadAll()
		if err_ != nil {
			return errs.New(errs.CodeIOReadFail, err_)
		}
		items := make([][]string, 0)
		for _, row := range rows {
			if len(row) < 15 {
				log.Error("columns invalid", zap.Int("num", len(row)), zap.String("name", inPath))
				continue
			}
			if row[0] == "TradingDay" {
				continue
			}
			// TradingDay,InstrumentID,UpdateTime,UpdateMillisec,LastPrice,Volume,BidPrice1,BidVolume1,
			// AskPrice1,AskVolume1,AveragePrice,Turnover,OpenInterest,UpperLimitPrice,LowerLimitPrice
			dateStr := row[0] + " " + row[2]
			timeObj, err_ := time.ParseInLocation(layout, dateStr, loc)
			if err_ != nil {
				return errs.New(errs.CodeRunTime, err_)
			}
			milliSecs, _ := strconv.ParseInt(row[3], 10, 64)
			timeMS := timeObj.UnixMilli() + milliSecs
			bidPrice1, _ := strconv.ParseFloat(row[6], 64)
			askPrice1, _ := strconv.ParseFloat(row[8], 64)
			avgPrice, _ := strconv.ParseFloat(row[10], 64)
			if bidPrice1 == 0 && askPrice1 == 0 && avgPrice == 0 {
				continue
			}
			// InstrumentID,Time,LastPrice,Volume,BidPrice1,BidVolume1,
			// AskPrice1,AskVolume1,AveragePrice,Turnover,OpenInterest,UpperLimitPrice,LowerLimitPrice
			item := append([]string{row[1], strconv.FormatInt(timeMS, 10)}, row[4:]...)
			items = append(items, item)
		}
		if len(items) == 0 {
			return nil
		}
		outWriter, err_ := out.Create(file.Name)
		if err_ != nil {
			return errs.New(errs.CodeIOWriteFail, err_)
		}
		csvWriter := csv.NewWriter(outWriter)
		err_ = csvWriter.WriteAll(items)
		if err_ != nil {
			return errs.New(errs.CodeIOWriteFail, err_)
		}
		csvWriter.Flush()
		return nil
	}
	makeOutPath := func(dirPath, name string) string {
		name = strings.ReplaceAll(name, "marketdatacsv", "")
		return filepath.Join(dirPath, name)
	}
	return convertFiles(args.InPath, args.OutPath, ".csv", makeOutPath, handleEntry)
}

func Build1mWithTicks(args *config.CmdArgs) *errs.Error {
	if args.InPath == "" {
		return errs.NewMsg(errs.CodeParamRequired, "--in is required")
	}
	if args.OutPath == "" {
		return errs.NewMsg(errs.CodeParamRequired, "--out is required")
	}
	names, err := FindPathNames(args.InPath, ".zip")
	if err != nil {
		return err
	}
	// 输入：根目录下只有年份的文件夹，每个年份文件夹下每个交易日一个zip压缩包，每个zip内以合约名称存储当日tick数据
	// 输出：根目录下每年一个zip压缩包，每个zip内以合约名称csv存储当年该合约的1m数据
	saveYear1m := func(year string) {
		if len(symKLines) == 0 || year == "" {
			return
		}
		outPath := fmt.Sprintf("%s/%s.zip", args.OutPath, year)
		out, err := os.Create(outPath)
		if err != nil {
			log.Error("create 1m zip fail", zap.String("year", year), zap.Error(err))
			return
		}
		defer out.Close()
		zipWriter := zip.NewWriter(out)
		defer zipWriter.Close()
		for symbol, items := range symKLines {
			outWriter, err_ := zipWriter.Create(symbol + ".csv")
			if err_ != nil {
				log.Error("create in zip fail", zap.String("year", year),
					zap.String("symbol", symbol), zap.Error(err))
				continue
			}
			sort.Slice(items, func(i, j int) bool {
				return items[i].Time < items[j].Time
			})
			rows := make([][]string, 0, len(items))
			for _, bar1m := range items {
				fltArr := []float64{bar1m.Open, bar1m.High, bar1m.Low, bar1m.Close, bar1m.Volume}
				row := make([]string, 0, 6)
				row = append(row, strconv.FormatInt(bar1m.Time, 10))
				for _, val := range fltArr {
					valStr := strconv.FormatFloat(math.Round(val*1000)/1000, 'f', -1, 64)
					row = append(row, valStr)
				}
				rows = append(rows, row)
			}
			csvWriter := csv.NewWriter(outWriter)
			err_ = csvWriter.WriteAll(rows)
			if err_ != nil {
				log.Error("write to zip fail", zap.String("year", year),
					zap.String("symbol", symbol), zap.Int("num", len(items)), zap.Error(err))
			}
			csvWriter.Flush()
		}
	}
	layout := "20060102 15:04:05"
	loc, err_ := time.LoadLocation("Asia/Shanghai")
	if err_ != nil {
		return errs.New(errs.CodeRunTime, err_)
	}
	tickBar := func(inPath string, row []string) (string, int64, float64, float64) {
		if len(row) == 15 {
			// TradingDay,InstrumentID,UpdateTime,UpdateMillisec,LastPrice,Volume,BidPrice1,BidVolume1,
			// AskPrice1,AskVolume1,AveragePrice,Turnover,OpenInterest,UpperLimitPrice,LowerLimitPrice
			bidPrice1, _ := strconv.ParseFloat(row[6], 64)
			askPrice1, _ := strconv.ParseFloat(row[8], 64)
			avgPrice, _ := strconv.ParseFloat(row[10], 64)
			if bidPrice1 == 0 && askPrice1 == 0 && avgPrice == 0 {
				return "", 0, 0, 0
			}
			symbol := row[1]
			dateStr := row[0] + " " + row[2]
			timeObj, err_ := time.ParseInLocation(layout, dateStr, loc)
			if err_ != nil {
				log.Error("invalid time", zap.String("date", dateStr), zap.String("name", inPath))
				return "", 0, 0, 0
			}
			milliSecs, _ := strconv.ParseInt(row[3], 10, 64)
			timeMS := timeObj.UnixMilli() + milliSecs
			price, _ := strconv.ParseFloat(row[4], 64)
			volume, _ := strconv.ParseFloat(row[5], 64)
			return symbol, timeMS, price, volume
		} else {
			// InstrumentID,Time,LastPrice,Volume,BidPrice1,BidVolume1,
			// AskPrice1,AskVolume1,AveragePrice,Turnover,OpenInterest,UpperLimitPrice,LowerLimitPrice
			timeMS, _ := strconv.ParseInt(row[1], 10, 64)
			price, _ := strconv.ParseFloat(row[2], 64)
			volume, _ := strconv.ParseFloat(row[3], 64)
			return row[0], timeMS, price, volume
		}
	}
	// 按年对文件名分组
	nameGrps := make([][]string, 0)
	var tmpNames []string
	oldYear := ""
	for _, name := range names {
		year := filepath.Base(filepath.Dir(name))
		if year != oldYear {
			if len(tmpNames) > 0 {
				nameGrps = append(nameGrps, tmpNames)
			}
			oldYear = year
			tmpNames = []string{name}
		} else {
			tmpNames = append(tmpNames, name)
		}
	}
	if len(tmpNames) > 0 {
		nameGrps = append(nameGrps, tmpNames)
	}
	// 每组分别处理消费
	for _, names = range nameGrps {
		year := filepath.Base(filepath.Dir(names[0]))
		outPath := fmt.Sprintf("%s/%s.zip", args.OutPath, year)
		if _, err_ := os.Stat(outPath); err_ == nil {
			// 已存在，跳过
			continue
		}
		log.Info("calc 1m kline from ticks", zap.String("year", year))
		totalNum := len(names) * core.StepTotal
		pBar := utils.NewPrgBar(totalNum, "tickTo1m")
		err = utils.ParallelRun(names, ConcurNum, func(name string) *errs.Error {
			fileInPath := filepath.Join(args.InPath, name)
			return ReadZipCSVs(fileInPath, pBar, func(inPath string, file *zip.File) *errs.Error {
				return build1mSymbolTick(inPath, file, tickBar)
			})
		})
		pBar.Close()
		log.Info("save 1m kline", zap.String("year", year))
		klineLock.Lock()
		saveYear1m(year)
		symKLines = make(map[string][]*banexg.Kline)
		klineLock.Unlock()
		if err != nil {
			return err
		}
	}
	return nil
}

func ReadZipCSVs(inPath string, pBar *utils.PrgBar, handle FuncReadZipItem) *errs.Error {
	r, err := zip.OpenReader(inPath)
	if err != nil {
		pBar.Add(core.StepTotal)
		return errs.New(errs.CodeIOReadFail, err)
	}
	defer r.Close()
	bar := pBar.NewJob(len(r.File))
	defer bar.Done()
	for _, f := range r.File {
		bar.Add(1)
		if f.FileInfo().IsDir() || !strings.HasSuffix(f.Name, ".csv") {
			continue
		}
		err2 := handle(inPath, f)
		if err2 != nil {
			return err2
		}
	}
	return nil
}

type tkInfo struct {
	symbol string
	timeMS int64   // 13位毫秒时间戳
	price  float64 // 最新价格
	volume float64 // 累计日成交量
}

func build1mSymbolTick(inPath string, file *zip.File, tickBar FuncTickBar) *errs.Error {
	if !isRawContract(file.Name) {
		return nil
	}
	fReader, err_ := file.Open()
	if err_ != nil {
		return errs.New(errs.CodeIOReadFail, err_)
	}
	rows, err_ := csv.NewReader(fReader).ReadAll()
	if err_ != nil {
		return errs.New(errs.CodeIOReadFail, err_)
	}
	oldMinMS := int64(0)
	sumVol := float64(0)
	var bar1m *banexg.Kline
	var oldSymbol string
	saveBar := func() {
		if bar1m == nil {
			return
		}
		klineLock.Lock()
		barRows, _ := symKLines[oldSymbol]
		if barRows == nil {
			barRows = make([]*banexg.Kline, 0, 10000)
		}
		symKLines[oldSymbol] = append(barRows, bar1m)
		klineLock.Unlock()
		sumVol = bar1m.Volume
	}
	// 文件名内可能有多个symbol的tick信息，这里进行排序
	ticks := make([]*tkInfo, 0, len(rows))
	for _, row := range rows {
		symbol, timeMS, price, volume := tickBar(inPath, row)
		if timeMS == 0 || !isRawContract(symbol) {
			continue
		}
		ticks = append(ticks, &tkInfo{symbol: symbol, timeMS: timeMS, price: price, volume: volume})
	}
	sort.Slice(ticks, func(i, j int) bool {
		a, b := ticks[i], ticks[j]
		if a.symbol != b.symbol {
			return a.symbol <= b.symbol
		} else {
			return a.timeMS <= b.timeMS
		}
	})
	// 对排序后的tick合并为K线
	for _, t := range ticks {
		curMinMS := utils.AlignTfMSecs(t.timeMS, 60000)
		price, volume := t.price, t.volume
		if bar1m == nil || oldMinMS == 0 || curMinMS > oldMinMS || oldSymbol != t.symbol {
			saveBar()
			oldSymbol = t.symbol
			curVol := t.volume - sumVol
			oldMinMS = curMinMS
			bar1m = &banexg.Kline{Time: curMinMS, Open: price, High: price, Low: price, Close: price, Volume: curVol}
		} else {
			if price > bar1m.High {
				bar1m.High = price
			} else if price < bar1m.Low {
				bar1m.Low = price
			}
			bar1m.Close = price
			bar1m.Volume = volume - sumVol
		}
	}
	saveBar()
	return nil
}
