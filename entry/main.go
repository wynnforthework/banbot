package entry

import (
	"flag"
	"fmt"
	"github.com/banbox/banbot/biz"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/data"
	"github.com/banbox/banbot/optmize"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"go.uber.org/zap"
	"os"
)

const VERSION = "0.1.1"

type FuncEntry = func(args *config.CmdArgs) *errs.Error
type FuncGetEntry = func(name string) (FuncEntry, []string)

func RunCmd() {
	if len(os.Args) < 2 {
		printAndExit()
	}
	args := os.Args[1:]
	runSubCmd(args, func(name string) (FuncEntry, []string) {
		var options []string
		var entry FuncEntry
		switch name {
		case "trade":
			options = []string{"stake_amount", "pairs", "stg_dir", "with_spider", "task_hash", "task_id"}
			entry = RunTrade
		case "backtest":
			options = []string{"timerange", "stake_amount", "pairs", "stg_dir", "cpu_profile", "mem_profile"}
			entry = RunBackTest
		case "spider":
			entry = RunSpider
		case "optimize":
			options = []string{"opt_rounds"}
			entry = optmize.RunOptimize
		case "kline":
			runKlineCmds(args[1:])
		case "tick":
			runTickCmds(args[1:])
		case "load_cal":
			options = []string{"in"}
			entry = biz.LoadCalendars
		case "cmp_orders":
			optmize.CompareExgBTOrders(args[1:])
			os.Exit(0)
		}
		return entry, options
	}, printAndExit)
}

func printAndExit() {
	tpl := `
banbot %v
please run with a subcommand:
	trade:      live trade
	backtest:   backtest with strategies and data
	spider:     start the spider
	kline:      run kline commands
	tick:		run tick commands
	cmp_orders: compare backTest orders with exchange orders
`
	log.Warn(fmt.Sprintf(tpl, VERSION))
}

func runKlineCmds(args []string) {
	runSubCmd(args, func(name string) (FuncEntry, []string) {
		var options []string
		var entry FuncEntry
		switch name {
		case "down":
			options = []string{"timerange", "pairs", "timeframes", "medium"}
			entry = RunDownData
		case "load":
			options = []string{"in", "cpu_profile", "mem_profile"}
			entry = LoadKLinesToDB
		case "export":
			options = []string{"out", "pairs", "timeframes", "adj", "tz"}
			entry = biz.ExportKlines
		case "purge":
			options = []string{"exg_real", "pairs", "timeframes"}
			entry = biz.PurgeKlines
		case "correct":
			entry = RunKlineCorrect
		case "adj_calc":
			options = []string{"out"}
			entry = RunKlineAdjFactors
		case "adj_export":
			options = []string{"out", "pairs", "tz"}
			entry = biz.ExportAdjFactors
		default:
			return nil, nil
		}
		return entry, options
	}, func() {
		tpl := `
banbot kline:
	down: 	download kline data from exchange
	load: 	load kline data from zip/csv files
	export:	export kline to csv files from db
	purge: 	purge/delete kline data with args
	correct: sync klines between timeframes
	adj_factor: recalculate adjust factors
please choose a valid action
`
		log.Warn(tpl)
	})
}

func runTickCmds(args []string) {
	runSubCmd(args, func(name string) (FuncEntry, []string) {
		var options []string
		var entry FuncEntry
		switch name {
		case "convert":
			options = []string{"in", "out", "cpu_profile", "mem_profile"}
			entry = data.RunFormatTick
		case "to_kline":
			options = []string{"in", "out", "cpu_profile", "mem_profile"}
			entry = data.Build1mWithTicks
		default:
			return nil, nil
		}
		return entry, options
	}, func() {
		tpl := `
banbot tick:
	convert: 	convert tick data format
	to_kline: 	build kline from ticks
please choose a valid action
`
		log.Warn(tpl)
	})
}

func runSubCmd(sysArgs []string, getEnt FuncGetEntry, printExit func()) {
	name, subArgs := sysArgs[0], sysArgs[1:]
	entry, options := getEnt(name)
	if entry == nil {
		printExit()
		return
	}
	var args config.CmdArgs
	var sub = flag.NewFlagSet(name, flag.ExitOnError)
	bindSubFlags(&args, sub, options...)
	err_ := sub.Parse(subArgs)
	if err_ != nil {
		log.Error("fail", zap.Error(err_))
		printExit()
		return
	}
	args.Init()
	err := entry(&args)
	if err != nil {
		panic(err)
	}
	os.Exit(0)
}

func bindSubFlags(args *config.CmdArgs, cmd *flag.FlagSet, opts ...string) {
	cmd.Var(&args.Configs, "config", "config path to use, Multiple -config options may be used")
	cmd.StringVar(&args.Logfile, "logfile", "", "Log to the file specified")
	cmd.StringVar(&args.DataDir, "datadir", "", "Path to data dir.")
	cmd.BoolVar(&args.NoDb, "nodb", false, "dont save orders to database")
	cmd.StringVar(&args.LogLevel, "level", "info", "set logging level to debug")
	cmd.BoolVar(&args.NoCompress, "no-compress", false, "disable compress for hyper table")
	cmd.BoolVar(&args.NoDefault, "no-default", false, "ignore default: config.yml, config.local.yml")
	cmd.IntVar(&args.MaxPoolSize, "max-pool-size", 0, "max pool size for db")

	for _, key := range opts {
		switch key {
		case "stake_amount":
			cmd.Float64Var(&args.StakeAmount, "stake-amount", 0.0, "Override `stake_amount` in config")
		case "stake_pct":
			cmd.Float64Var(&args.StakePct, "stake-pct", 0.0, "Override `stake_pct` in config")
		case "pairs":
			cmd.StringVar(&args.RawPairs, "pairs", "", "comma-separated pairs")
		case "stg_dir":
			cmd.Var(&args.StrategyDirs, "stg-dir", "dir path for strategies")
		case "with_spider":
			cmd.BoolVar(&args.WithSpider, "spider", false, "start spider if not running")
		case "timerange":
			cmd.StringVar(&args.TimeRange, "timerange", "", "Specify what timerange of data to use")
		case "timeframes":
			cmd.StringVar(&args.RawTimeFrames, "timeframes", "", "comma-seperated timeframes to use")
		case "medium":
			cmd.StringVar(&args.Medium, "medium", "", "data medium:db,file")
		case "tables":
			cmd.StringVar(&args.RawTables, "tables", "", "db tables, comma-separated")
		case "force":
			cmd.BoolVar(&args.Force, "force", false, "skip confirm")
		case "task_hash":
			cmd.StringVar(&args.TaskHash, "task-hash", "", "hash code to use")
		case "cpu_profile":
			cmd.BoolVar(&args.CPUProfile, "cpu-profile", false, "enable cpu profile")
		case "mem_profile":
			cmd.BoolVar(&args.MemProfile, "mem-profile", false, "enable memory profile")
		case "task_id":
			cmd.IntVar(&args.TaskId, "task-id", 0, "task")
		case "in":
			cmd.StringVar(&args.InPath, "in", "", "input file or directory")
		case "out":
			cmd.StringVar(&args.OutPath, "out", "", "output file or directory")
		case "adj":
			cmd.StringVar(&args.AdjType, "adj", "", "pre/post/none for kline")
		case "tz":
			cmd.StringVar(&args.TimeZone, "tz", "", "timeZone, default: utc")
		case "exg_real":
			cmd.StringVar(&args.ExgReal, "exg_real", "", "real exchange")
		case "opt_rounds":
			cmd.IntVar(&args.OptRounds, "opt-rounds", 30, "rounds num for single optimize job")
		default:
			log.Warn(fmt.Sprintf("undefined argument: %s", key))
			os.Exit(1)
		}
	}
}
