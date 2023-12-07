package main

import (
	"flag"
	"fmt"
	"github.com/anyongjin/banbot/cmd"
	"github.com/anyongjin/banbot/log"
	"gopkg.in/yaml.v3"
	"os"
)

var subHelp = map[string]string{
	"trade":     "live trade",
	"backtest":  "backtest with strategies and data",
	"down_data": "download kline data",
	"down_ws":   "download websocket data",
	"dbcmd":     "run db command",
	"spider":    "start the spider",
}

const VERSION = "0.1.1"

func main() {
	if len(os.Args) < 2 {
		printAndExit()
	}
	cmdName := os.Args[1]
	var args cmd.CmdArgs

	var sub = flag.NewFlagSet(cmdName, flag.ExitOnError)
	switch cmdName {
	case "trade":
		bindSubFlags(&args, sub, []string{"stake_amount", "pairs", "stg_dir", "with_spider", "task_hash", "task_id"})
	case "backtest":
		bindSubFlags(&args, sub, []string{"timerange", "stake_amount", "pairs", "stg_dir"})
	case "down_data":
		bindSubFlags(&args, sub, []string{"timerange", "pairs", "timeframes", "medium"})
	case "down_ws":
		break
	case "dbcmd":
		bindSubFlags(&args, sub, []string{"action", "tables", "force"})
	case "spider":
		break
	default:
		printAndExit()
	}

	err := sub.Parse(os.Args[2:])
	args.Init()
	if err != nil {
		fmt.Printf("Error: %v", err)
		printAndExit()
	}
	log.SetupByArgs(&args)
	data, err := yaml.Marshal(args)
	fmt.Println("result: \n", string(data))
}

func bindSubFlags(args *cmd.CmdArgs, cmd *flag.FlagSet, opts []string) {
	cmd.Var(&args.Configs, "config", "config path to use, Multiple -config options may be used")
	cmd.StringVar(&args.Logfile, "logfile", "", "Log to the file specified")
	cmd.StringVar(&args.DataDir, "datadir", "", "Path to data dir.")
	cmd.BoolVar(&args.NoDb, "nodb", false, "dont use database")
	cmd.BoolVar(&args.Debug, "debug", false, "set logging level to debug")
	cmd.BoolVar(&args.NoCompress, "no-compress", false, "disable compress for hyper table")
	cmd.BoolVar(&args.NoDefault, "no-default", false, "ignore default: config.yml, config.local.yml")

	for _, key := range opts {
		switch key {
		case "stake_amount":
			cmd.Float64Var(&args.StakeAmount, "stake-amount", 0.0, "Override `stake_amount` in config")
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
		case "action":
			cmd.StringVar(&args.Action, "action", "", "db action name")
		case "tables":
			cmd.StringVar(&args.RawTables, "tables", "", "db tables, comma-separated")
		case "force":
			cmd.BoolVar(&args.Force, "force", false, "skip confirm")
		case "task_hash":
			cmd.StringVar(&args.TaskHash, "task-hash", "", "hash code to use")
		case "task_id":
			cmd.IntVar(&args.TakId, "task-id", 0, "task")
		default:
			fmt.Println("undefined argument:", key)
			os.Exit(1)
		}
	}
}

func printAndExit() {
	fmt.Printf("banbot %v\nplease run with a subcommand:\n", VERSION)
	for k, v := range subHelp {
		fmt.Println("  ", k)
		fmt.Println("\t", v)
	}
	os.Exit(1)
}
