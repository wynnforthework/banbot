package entry

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/banbox/banbot/biz"
	"github.com/banbox/banbot/config"
	"github.com/banbox/banbot/core"
	"github.com/banbox/banbot/data"
	"github.com/banbox/banbot/opt"
	"github.com/banbox/banbot/web"
	"github.com/banbox/banexg/errs"
	"github.com/banbox/banexg/log"
	"go.uber.org/zap"
)

type FuncEntry = func(args *config.CmdArgs) *errs.Error
type FuncGetEntry = func(name string) (FuncEntry, []string)

func RunCmd() {
	defer func() {
		if r := recover(); r != nil {
			log.Error("banbot panic", zap.Any("error", r))
			core.RunExitCalls()
			os.Exit(1)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 在goroutine中等待信号
	go func() {
		<-sigChan
		if core.Ctx != nil {
			core.Ctx.Done()
			core.StopAll()
		}
		core.RunExitCalls()
		os.Exit(0)
	}()

	args := os.Args[1:]
	if len(args) == 0 {
		runWeb(args)
		return
	}
	runSubCmd(args, func(name string) (FuncEntry, []string) {
		var options []string
		var entry FuncEntry
		switch name {
		case "trade":
			options = []string{"stake_amount", "pairs", "with_spider", "task_hash", "task_id"}
			entry = RunTrade
		case "backtest":
			options = []string{"out", "timerange", "stake_amount", "pairs", "prg", "separate", "cpu_profile", "mem_profile"}
			entry = RunBackTest
		case "spider":
			entry = RunSpider
		case "optimize":
			options = []string{"out", "opt_rounds", "sampler", "picker", "each_pairs", "concur"}
			entry = opt.RunOptimize
		case "init":
			entry = runInit
		case "bt_opt":
			options = []string{"review_period", "run_period", "opt_rounds", "sampler", "picker", "each_pairs",
				"concur", "alpha", "pair_picker"}
			entry = opt.RunBTOverOpt
		case "kline":
			runKlineCmds(args[1:])
		case "tick":
			runTickCmds(args[1:])
		case "tool":
			runToolCmds(args[1:])
		case "web":
			runWeb(args[1:])
		default:
			return nil, nil
		}
		return entry, options
	}, func(e error) {
		if e == nil {
			if strings.HasPrefix(args[0], "-") {
				runWeb(args)
			} else {
				log.Warn("unknown subcommand: " + args[0])
				printMainHelp()
			}
		} else {
			log.Error("parse command args fail", zap.String("cmd", args[0]), zap.Error(e))
		}
	})
}

func runWeb(args []string) {
	err_ := web.RunDev(args)
	if err_ != nil {
		panic(err_)
	}
	os.Exit(0)
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
    init:       initialize config.yml/config.local.yml in BanDataDir
    bt_opt:     rolling backtest with hyperparameter optimization
    kline:      run kline commands
    tick:       run tick commands
    tool:       run tools
    web:        run web ui
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
			entry = runExportData
		case "purge":
			options = []string{"exg_real", "pairs", "timeframes"}
			entry = runPurgeData
		case "correct":
			options = []string{"pairs"}
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
	}, func(e error) {
		if e != nil {
			log.Error("parse command args fail", zap.String("cmd", args[0]), zap.Error(e))
			return
		}
		log.Warn("unknown subcommand: " + args[0])
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
	}, func(e error) {
		if e != nil {
			log.Error("parse command args fail", zap.String("cmd", args[0]), zap.Error(e))
			return
		}
		log.Warn("unknown subcommand: " + args[0])
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
			options = []string{"review_period", "run_period", "opt_rounds", "sampler", "each_pairs", "concur",
				"picker", "pair_picker"}
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
	}, func(e error) {
		if e != nil {
			log.Error("parse command args fail", zap.String("cmd", args[0]), zap.Error(e))
			return
		}
		log.Warn("unknown subcommand: " + args[0])
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

func runSubCmd(sysArgs []string, getEnt FuncGetEntry, fallback func(e error)) {
	name, subArgs := sysArgs[0], sysArgs[1:]
	entry, options := getEnt(name)
	if entry == nil {
		fallback(nil)
		return
	}
	var args config.CmdArgs
	var sub = flag.NewFlagSet(name, flag.ExitOnError)
	bindSubFlags(&args, sub, options...)
	err_ := sub.Parse(subArgs)
	if err_ != nil {
		fallback(err_)
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
		case "prg":
			cmd.StringVar(&args.PrgOut, "prg", "", "prefix for progress in stdout")
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
		case "pair_picker":
			cmd.StringVar(&args.PairPicker, "pair-picker", "", "min sharpe val for pairs in bt_opt mode")
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
