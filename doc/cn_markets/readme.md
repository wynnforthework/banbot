# 国内股票、期货等市场
## 行情数据源
### tushare api
https://tushare.pro/document/2  
大部分接口需要积分，积分一年有效。支持python/api获取数据。

### akshare/aktools api (新浪、腾讯)
https://akshare.akfamily.xyz/introduction.html  
包含股票、期货、外汇、宏观、东财、新浪、腾讯、港美股、基金等。支持python/api获取数据。  

### 天勤TqSdk python
https://doc.shinnytech.com/tqsdk/latest/intro.html  
包含股票、期货、港美股、基金等，支持回测、实盘。支持python。

### 立得行情数据API
http://ldhqsj.com/  
收费数据。包含股票、期货、外汇、指数、基金、债券等。支持api获取数据。

## AKShare 数据接入
启动本地api：
```shell
python -m aktools
```

## 期货交易指引
[文档](future.md)

## 实盘程序化交易
### 期货程序化交易
首推CTP，开通门槛低，接口全面。[CTP接入文档](./ctp.md)

### 股票程序化交易
股票监管较严，不允许公网api下单。目前主要有柜台api、文件单、量化交易终端、破解接口四种方式。  

**柜台api**  
服务器托管内网运行，稳定合规，速度最快。资金门槛较高，需要自行对接api。  
中泰XTP，恒生UFT，顶点HTS，金证FGS，宽睿OES，华宝LTS（可搜索柜台极速交易系统对比）  

**文件单**  
可本地接入，灵活，门槛较高，处于灰色地带。  
迅投PB、恒生PB、ptrade  

**量化交易终端**  
使用知名软件终端，使用内置语言，灵活度受限。  
tbquant、聚宽、迅投qmt、恒生ptrade、同花顺、TradeStation

**破解接口**  
无资金门槛，不合规，不稳定。  
通达信破解版、券商网页交易、同花顺api  
