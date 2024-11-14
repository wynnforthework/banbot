
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
strat
  orm utils
goods
  orm exg 
biz:
  exg orm strat goods data
optmize
  biz data orm goods strat
live 
  biz data orm goods strat
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
    AccOdMgrs // 订单簿对象，必定不为空
    AccLiveOdMgrs // 实盘订单簿对象，实盘时不为空
    AccWallets // 钱包
    
data
    Spider // 爬虫

orm
    HistODs // 已平仓的订单：全部出场，仅回测使用
    AccOpenODs // 打开的订单：尚未提交、已提交未入场、部分入场，全部入场，部分出场
    AccTriggerODs // 尚未提交的限价入场单，等待轮询提交到交易所，仅实盘使用
    AccTaskIDs // 当前任务ID
    AccTasks // 当前任务

exg
    Default  // 交易所对象

strat
    StagyMap // 策略注册map
    Versions // 策略版本
    Envs // 涉及的所有K线环境：Pair+TimeFrame
    PairTFStags // 标的+TF+策略
    AccJobs // 涉及的所有标的
    AccInfoJobs // 涉及的所有辅助信息标的
```
注意：所有Acc开头的变量都是支持多账户的map

# 常见问题
### 如何进行函数性能测试？
回测时添加`-cpu-profile`参数，启用性能测试，输出`cpu.profile`到回测目录下。然后执行下面命令可以查看结果
```shell
go tool pprof -http :8080 cpu.profile
```
### 如何发布go模块新版本？
```shell
git tag v1.0.0
git push origin --tags
```
### 如何引用本地go模块？
1. 被引用模块执行`go mod init`添加`go.mod`文件，修改`module`后的模块名
2. 执行上面`git tag`添加新版本
3. 在当前项目（和依赖当前项目的入口项目）添加依赖：`go get 模块名@version`
4. 在当前项目（和依赖当前项目的入口项目）的`go.mod`中添加`replace`指令，改为本地绝对或相对路径

### json
It is not recommended to replace `encoding/json` with [sonic](https://github.com/bytedance/sonic/issues/574). The binary file will increase by 15M (on Windows)

# TODO
1. 将策略注入改为接口，以便允许从结构体继承。
