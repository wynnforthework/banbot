# 部署到linux
## 生成linux可执行文件
在Windows的Powershell中生成：
```shell
-- 生成banbot.o包含了策略，用于启动机器人，爬虫
cd [项目路径]/banstagy
$env:GOARCH="amd64"
$env:GOOS="linux"
go build -o banbot.o

-- windows
$env:GOARCH="amd64"
$env:GOOS="windows"
go build -o banbot.exe

```
## 服务器环境准备
### 授予可执行权限
将生成的`banbot`和`banstagy`文件上传到服务器合适的目录下，此处以`/app`为例。
赋予可执行权限：
```shell
chmod +x /app/banbot.o
mkdir -p /app/bandata
mkdir -p /app/banstagy
```
### 环境变量配置
```shell
vim ~/.bashrc
```
```shell
export BanDataDir=/app/bandata
export BanStgyDir=/app/banstagy
```
执行下面命令立刻生效：
```shell
source ~/.bashrc
```
### 配置文件部署
在`/app/bandata`下创建`config.yml`和`config.local.yml`配置文件。  
`config.yml`用于存放非敏感的，所有用户最通用的配置。  
`config.local.yml`优先级高于`config.yml`，可存放一些敏感信息。  

### 数据库初始化
请查看[数据库初始化指引](./timescaledb.md)

### 启动爬虫和机器人
```shell
-- 启动爬虫
nohup /app/banbot.o spider > /app/spider.log 2>&1 &

-- 启动机器人
nohup /app/banbot.o trade > /app/bot.log 2>&1 &
```
