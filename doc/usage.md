
# Q&A 常见问题
### 如何在回测时模拟滑点？
可通过设置`config.bt_net_cost`来模拟滑点，这是回测时下单延迟，单位：秒，默认30
### 如何确认实盘和回测一致？
您可将回测相同的配置改为实盘，运行一段时间，大概有几百笔订单之后，停止实盘，从交易所账户-订单-导出委托历史为excel文件。  
然后将回测配置`config.timerange`改为实盘的开始和结束时间戳，如`1711629900000-1711632600000`  
然后将回测配置`config.pairs`固定为实盘的标的列表，同时其他重要参数和实盘保持一致。  
运行回测，得到`orders_[taskID].csv`订单记录文件。  
然后执行下面命令，自动从交易所委托excel中解析订单，并与回测订单对比。  
您可在实盘excel文件同目录下查看`cmp_orders.csv`文件即实盘和回测对比结果。
```shell
banbot cmp_orders --exg-path=[实盘委托记录.xlsx] --bt-path=[orders_-1.csv]
```
### 如何在机器人停止时发邮件通知？
```shell
yum install sendmail sendmail-cf mailx

# 设置发件人信息
vim /etc/mail.rc

set from=sender_addr
set smtp=smtp.server.com
set smtp-auth-user=sender_addr
set smtp-auth-password=sender_password
set smtp-auth=login

# 查看邮件
tail /var/log/maillog
```
然后将`check_bot.sh`复制到服务器，并执行`chmod +x check_bot.sh`授予可执行权限。  
然后在corntab中每隔5分钟执行脚本检查状态：
```text
3-58/5 * * * * /path_to/check_bot.sh bot1 user1@xxx.com user2@xxx.com
```
### 如何进行策略的超参数调优？
策略的超参数对收益稳定性至关重要。可在策略中定义各个超参数的上下限，然后使用超参数优化方法自动搜索最佳超参数组合。  
1. 策略中定义待优化超参数：`pol.Def("ma", 10, core.PNorm(5, 400))`；  
上面定义了一个正态分布的超参数，默认值10，上下限分别400和5，并自动使用默认值作为期望值；（也可使用`PNormF`指定期望值和倍率`Rate`）
2. 运行超参数优化；`banbot optimize --nodb -opt-rounds 40 -sampler tpe`；  
其中`opt-rounds`指定单轮任务搜索轮次，`sampler`指定搜索方法，支持:tpe/bayes/random/cmaes/ipop-cmaes/bipop-cmaes
3. 查看日志并选定最佳参数组合：  
打开日志`backtest/task_-1/optimize.log`，将每个策略的最佳参数复制到`config/run_policy/[?]/params`下，并设定`dirt`为long或short
