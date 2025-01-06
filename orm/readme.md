# 数据库概述
需要存储到数据库的有三类：K线数据、交易数据、UI相关数据。  
为确保性能，K线使用TimeScaledb存储。  
为确保灵活性，交易数据(ormo)和UI相关数据(ormu)使用独立的sqlite文件存储。  
ormo/ormu依赖orm，不可反向依赖，避免出现依赖环

# sqlc
数据库相关的访问代码全部使用`sqlc`从sql文件生成go代码。  
`kline1m`等不需要sqlc生成的，全部写到schema2.sql中。  
`sqlc`不支持windows直接使用，需要启动Docker Desktop，然后在命令行执行下面命令：
```shell
docker run --rm -v "E:/trade/go/banbot/orm:/src" -w /src sqlc/sqlc generate
```

# 从proto生成go代码
安装protoc:
```shell
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
```
生成：
```shell
protoc --go_out=. --go_opt=paths=source_relative kdata.proto
```
注意将生成后的`kdata.pb.go`中`package __`改为`package orm`
