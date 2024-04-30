依据中国国内监管要求，客户无法直连交易所系统，中间必须经过期货公司（Broker）的系统，这便是柜台系统。  
期货公司会有多套柜台系统，在功能上可以分为主席和次席系统。主席系统功能全面，支持出入金，盘后结算等，讲究的是高吞吐量与高可靠性，一般客户都是在主席系统上交易。次席系统一般只做下单及撤单用，讲究的是低延迟穿透时间，一般为对时延要求较高的客户准备。  
例如CTP (Comprehensive Transaction Platform, 综合交易平台)即是上期所子公司上期技术开发的一套主席系统。   
中国国内有六大期货交易所，CTP是上期所子公司提供的接口，原生是C++，因极低的延时，开放式接口，支持全部期货交易所，是期货交易的事实标准。  

# CTP API对接
CTP Api分为行情和交易两类，两者是完全独立的，可以只使用其中一个。  
```shell
thostmduserapi.dll  # 行情库文件
thosttraderapi.dll  # 交易库文件
```
行情类库只提供实时切片数据，无历史数据，需要从切片数据合成实时K线。  
交易类库`thosttraderapi`会提供`TraderApi`和`TradeSpi`两个类，前者用于创建并发送请求，后者用于处理CTP的回复信息。`Api`和`Spi`的方法都是一一对应的。  

## tick与bar合成
tick数据一般是指市场上的逐笔数据，一笔成交就会产生一笔行情。目前国内期货交易所还不支持推送这种逐笔的数据，只推送切片（快照）数据。切片数据是指将一定时间内的逐笔数据统计成一个快照发出，一般是1秒2笔。但郑商所有点特殊，可能是1s多笔。CTP发出的行情正是转发的交易所的行情，所以也是500ms一次快照。一般业内也将这个快照数据称之为tick。  

### bar最高、最低价问题
CTP不推送K线数据，所以需要客户自己根据tick数据计算得出。CTP推送的tick中只包含当日最高价和当日最低价，并非此tick时间范围内的最高最低。所以从tick合成K线时，只能从tick的最新价格计算bar的最高价和最低价。  
如果两个tick中间出现一笔成交价高于（或低于）两个tick最新价，这个更高价实际上无法发现（除非是截止当时的当日最高）。  

### 不同软件bar不一致问题
|      | 天勤快期                    | 文华财经                    | 博易大师                    | TBQuant |
|------|-------------------------|-------------------------|-------------------------|---------|
| 数据   | 左闭右闭                    | 左闭右开                    | 左闭右开                    |         |
| 显示时间 | bar开始时间                 | bar开始时间                 | bar结束时间                 | bar开始时间 |
| 开盘价  |                         |                         |                         |         |
| 收盘价  | 21:10:00.0 tick的最新价     | 21:09:59.5 tick的最新价     | 21:09:59.5 tick的最新价     |         |
| 成交量  | 21:10:00.0 — 21:05:00.0 | 21:09:59.5 — 21:04:59.5 | 21:09:59.5 — 21:04:59.5 |         |


## 行情订阅QA
**订阅成功但没有行情**  
1. CTP订阅时无论合约ID是否存在，都返回成功。所以要检查合约ID是否正确。
2. 检查此合约是否已到期。

**非交易时段收到行情？**  
日盘盘前可能会收到行情，是因为CTP日盘起动时会重演夜盘的流水，所以有可能会将夜盘的行情再推送一遍。日盘结束后也会收到行情，这是交易所结算完成发出的行情，这里面的结算价字段是当日结算价，一般推送时间在3点~3点半。建议按照交易时间过滤掉这些非盘中行情，以免影响交易逻辑。

**UpdateMillisec毫秒时间戳有效吗？**  
上期/能源/中金三个交易所会出现0和500两种值，大商所值是切片时真实毫秒时间，郑州该值都是0。

**行情均价AveragePrice字段有什么坑？**  
AveragePrice这个字段除郑商所可以直接拿过来用之外，其它四大交易所要除以合约乘数，才是真正的均价。至于合约乘数是什么百度吧，可以通过用交易API查询合约获得，或者去交易所网站上查询获得。

## CTP登录信息
### BrokerID
简称期商编码，是指该期货公司在CTP系统上的编码，为四位数。例如海通期货是8000。
### TradeFront, MarketFront
TradeFront是指CTP系统的交易前置IP地址，客户用来连接下单撤单等；MarketFront是指行情前置IP地址，用来订阅收取行情。
### InvestorID（UserID,InvestUnitID）
投资者代码，是指该客户在CTP系统上的唯一ID，在期货公司开户后由期货公司分配得到。UserID是操作员代码，InvestUnitID是投资单元代码，普通投资者遇到要填这两个值的，直接填InvestorID即可。
### Password
开户时设置的密码。需要注意的是开户完首次登录CTP系统需要修改密码，在期货公司官网上下载快期客户端登录，点修改密码就可以。
### AppID
客户终端软件代码。
### AuthCode
客户终端软件认证码。

# CTP实盘
### 中泰XTP
https://xtp.zts.com.cn/doc/api/xtpDoc  
支持期货、股票程序化交易，实时行情，性能最好，内网托管交易。  


# 模拟仿真
CTP官方有提供SimNow仿真平台，因受规章限制较多，很多时段不开放，有时候一个多月不开放。故第三方的openctp作为跟SimNow类似的仿真环境，受众也很多。  

## OpenCTP
OpenCTP相比SimNow(CTP)的优点：  
* 7X24小时可用
* 除了期货期权，还提供A股股票、债券、基金、港美股、外盘期货等其他市场行情
* 支持市价单、支持负价交易
* 提供三套模拟环境
* 支持任意切换柜台：CTP、中泰证券XTP、华鑫证券奇点STP、东方财富EMT、盈透证券

### OpenCTP 参考
[OpenCTP github](https://github.com/openctp/openctp)  
[OpenCTP 公众号](https://mp.weixin.qq.com/s?__biz=Mzk0ODI0NDE2Ng==&mid=2247483659&idx=1&sn=20d85106f06531bc2246241467c7d71b&chksm=c36bdaa2f41c53b4402e0e2fa4e4a12c38f977196f787914ddbf234444cf0d3056f984df688f&token=201078644&lang=zh_CN#rd)  
[OpenCTP 接口开发文档](https://github.com/openctp/openctp/tree/master/docs)


# CTP 参考资料
[CTP接口对接系统概要](https://zhuanlan.zhihu.com/p/397359483)  
[公众号景色的CTP对接文章](https://mp.weixin.qq.com/mp/appmsgalbum?__biz=Mzg5MjEwNDEwMQ==&action=getalbum&album_id=1545992091560968194&subscene=159&subscene=&scenenote=https%3A%2F%2Fmp.weixin.qq.com%2Fs%2Fp1bxp5uBHRbH3yJoXY5ifw&nolastread=1)  
[公众号秋水的CTP对接文章](https://mp.weixin.qq.com/mp/appmsgalbum?__biz=MzAxOTQ2ODA3OA==&action=getalbum&album_id=1501810151681523713&scene=173&from_msgid=2247483762&from_itemidx=1&count=3&nolastread=1)  
[知乎krenx的CTP柜台系统技术文章](https://www.zhihu.com/column/c_1356686503654109184)  
[vnpy的CTP社区](https://www.vnpy.com/forum/forum/5-ctp)  
[CTPAPI哪些字段可以用来标识订单？](https://zhuanlan.zhihu.com/p/461809304)  
[CTP接口量化交易资料汇总](https://zhuanlan.zhihu.com/p/607325008)  
[一创聚宽回测介绍](https://ycjq.95358.com/api#%E5%9B%9E%E6%B5%8B%E7%8E%AF%E5%A2%83)  