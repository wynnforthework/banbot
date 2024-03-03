
## Package Dependencies
```text
core
  --
btime
  core
utils
  core btime
config
  core btime utils
exg
  config utils core
orm
  exg config  
data
  orm exg config 
strategy
  orm utils
goods
  strategy orm exg 
biz:
  exg orm strategy goods data
optmize
  biz data orm goods strategy
live 
  biz data orm goods strategy
entry
  optmize live data 
```

# Important Global Variables
```text
core
    Ctx  // 全局上下文，可select <- core.Ctx响应全局退出事件
    StopAll // 发出全局退出事件
    NoEnterUntil  // 在给定截止时间戳之前禁止开单
    
biz
    OdMgr // 订单簿对象，必定不为空
    OdMgrLive // 实盘订单簿对象，实盘时不为空
    Wallets // 钱包
    IsWatchBalance
    IsWatchAccConfig
    
data
    Main // 当前数据源
    Spider // 爬虫

orm
    OpenODs // 打开的订单：尚未提交、已提交未入场、部分入场，全部入场，部分出场
    HistODs // 已平仓的订单：全部出场，仅回测使用
    TriggerODs // 尚未提交的限价入场单，等待轮询提交到交易所，仅实盘使用
    TaskID // 当前任务ID
    Task // 当前任务

exg
    Default  // 交易所对象

strategy
    StagyMap // 策略注册map
    Versions // 策略版本
    Envs // 涉及的所有K线环境：Pair+TimeFrame
    Jobs // 涉及的所有标的
    InfoJobs // 涉及的所有辅助信息标的
    PairTFStags // 标的+TF+策略
```
