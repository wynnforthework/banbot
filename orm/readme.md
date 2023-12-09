# sqlc
数据库相关的访问代码全部使用`sqlc`从sql文件生成go代码。  
`kline1m`等不需要sqlc生成的，全部写到schema2.sql中。  
`sqlc`不支持windows直接使用，需要启动Docker Desktop，然后在命令行执行下面命令：
```shell
docker run --rm -v "E:/trade/go/banbot/orm:/src" -w /src sqlc/sqlc generate
```
