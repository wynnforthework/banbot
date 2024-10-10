package entry

import (
	"flag"
	"fmt"
	"github.com/banbox/banbot/biz"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/data"
	"github.com/banbox/banbot/opt"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"go.uber.org/zap"
	"os"
	"strings"
)

type FuncEntry = func(args *config.CmdArgs) *errs.Error
type FuncGetEntry = func(name string) (FuncEntry, []string)

func RunCmd() {
	if len(os.Args) < 2 {
		printMainHelp()
		return
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
			options = []string{"timerange", "stake_amount", "pairs", "stg_dir", "separate", "cpu_profile", "mem_profile"}
			entry = RunBackTest
		case "spider":
			entry = RunSpider
		case "optimize":
			options = []string{"out", "opt_rounds", "sampler", "picker", "each_pairs", "concur"}
			entry = opt.RunOptimize
		case "bt_opt":
			options = []string{"review_period", "run_period", "opt_rounds", "sampler", "picker", "each_pairs",
				"concur", "alpha"}
			entry = opt.RunBTOverOpt
		case "kline":
			runKlineCmds(args[1:])
		case "tick":
			runTickCmds(args[1:])
		case "tool":
			runToolCmds(args[1:])
		}
		return entry, options
	}, printMainHelp)
}

func printMainHelp() {
	tpl := `
args: %s
banbot %v
please run with a subcommand:
    trade:      live trade
    backtest:   backtest with strategies and data
    spider:     start the spider
    optimize:   run hyper parameters optimization
    bt_opt:     rolling backtest with hyperparameter optimization
    kline:      run kline commands
    tick:       run tick commands
    tool:       run tools
`
	log.Warn(fmt.Sprintf(tpl, strings.Join(os.Args, " "), core.Version))
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
    down:       download kline data from exchange
    load:       load kline data from zip/csv files
    export:     export kline to csv files from db
    purge:      purge/delete kline data with args
    correct:    sync klines between timeframes
    adj_calc:   recalculate adjust factors
    adj_export: export adjust factors to csv
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
    convert:     convert tick data format
    to_kline:    build kline from ticks
please choose a valid action
`
		log.Warn(tpl)
	})
}

func runToolCmds(args []string) {
	runSubCmd(args, func(name string) (FuncEntry, []string) {
		var options []string
		var entry FuncEntry
		switch name {
		case "collect_opt":
			options = []string{"in", "picker"}
			entry = opt.CollectOptLog
		case "test_pickers":
			options = []string{"review_period", "run_period", "opt_rounds", "sampler", "each_pairs", "concur", "picker"}
			entry = opt.RunRollBTPicker
		case "load_cal":
			options = []string{"in"}
			entry = biz.LoadCalendars
		case "cmp_orders":
			opt.CompareExgBTOrders(args[1:])
			os.Exit(0)
		case "data_server":
			options = []string{"mem_profile"}
			entry = biz.RunDataServer
		case "calc_perfs":
			options = []string{"in", "in_type", "out"}
			entry = data.CalcFilePerfs
		case "corr":
			options = []string{"out", "out_type", "timeframes", "batch_size", "run_every"}
			entry = biz.CalcCorrelation
		default:
			return nil, nil
		}
		return entry, options
	}, func() {
		log.Warn(`
banbot tool:
    collect_opt:     collect result of optimize, and print in order
    test_pickers:    test pickers in roll backtest
    load_cal:        load calenders
    cmp_orders:      compare backTest orders with exchange orders
    data_server:     serve a grpc server as data feeder
    calc_perfs:      calculate sharpe/sortino ratio for input data
    corr:            calculate correlation matrix for symbols 
`)
	})
}

func runSubCmd(sysArgs []string, getEnt FuncGetEntry, printExit func()) {
	name, subArgs := sysArgs[0], sysArgs[1:]
	entry, options := getEnt(name)
	if entry == nil {
		log.Warn("unknown subcommand: " + name)
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
		case "in_type":
			cmd.StringVar(&args.InType, "in-type", "", "input data type")
		case "out":
			cmd.StringVar(&args.OutPath, "out", "", "output file or directory")
		case "adj":
			cmd.StringVar(&args.AdjType, "adj", "", "pre/post/none for kline")
		case "tz":
			cmd.StringVar(&args.TimeZone, "tz", "", "timeZone, default: utc")
		case "exg_real":
			cmd.StringVar(&args.ExgReal, "exg-real", "", "real exchange")
		case "opt_rounds":
			cmd.IntVar(&args.OptRounds, "opt-rounds", 30, "rounds num for single optimize job")
		case "sampler":
			cmd.StringVar(&args.Sampler, "sampler", "bayes", "hyper optimize method, tpe/bayes/random/cmaes/ipop-cmaes/bipop-cmaes")
		case "picker":
			cmd.StringVar(&args.Picker, "picker", "good3", "Method for selecting targets from multiple hyperparameter optimization results")
		case "alpha":
			cmd.Float64Var(&args.Alpha, "alpha", 1, "ma alpha for calculating ema in hyperOpt")
		case "each_pairs":
			cmd.BoolVar(&args.EachPairs, "each-pairs", false, "run for each pairs")
		case "concur":
			cmd.IntVar(&args.Concur, "concur", 1, "Concurrent Number")
		case "review_period":
			cmd.StringVar(&args.ReviewPeriod, "review-period", "3y", "review period, default: 3 years")
		case "run_period":
			cmd.StringVar(&args.RunPeriod, "run-period", "6M", "run period, default: 6 months")
		case "batch_size":
			cmd.IntVar(&args.BatchSize, "batch-size", 0, "batch size for task")
		case "run_every":
			cmd.StringVar(&args.RunEveryTF, "run-every", "", "run every ? timerange")
		case "out_type":
			cmd.StringVar(&args.OutType, "out-type", "", "output data type")
		case "separate":
			cmd.BoolVar(&args.Separate, "separate", false, "run policy separately for backtest")
		default:
			log.Warn(fmt.Sprintf("undefined argument: %s", key))
			os.Exit(1)
		}
	}
}
