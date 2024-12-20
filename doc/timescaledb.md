# TimeScaleDb安装
## Windows安装
先安装[OpenSSL 3.x](https://slproweb.com/products/Win32OpenSSL.html)  
安装TimescaleDb的2.13及以后版本。安装Postgresql 15及之后版本。  

## Linux中安装
必须使用CentOS 7，测试CentOS8安装时遇到很多不兼容错误。
可直接按照官方文档中Red Hat 7的安装指引：  
[Install TimescaleDB on Linux](https://docs.timescale.com/self-hosted/latest/install/installation-linux/)
## Docker安装
```shell
docker pull timescale/timescaledb:latest-pg16

# 启动容器：
docker run -d --name timescaledb -p 5432:5432 -v /opt/pgdata:/var/lib/postgresql/data -e POSTGRES_PASSWORD=Uf6CVdsZ3Dc timescale/timescaledb:latest-pg16

# 进入数据库：
docker exec -it timescaledb psql -U postgres -h localhost [-d bantd]

# 创建数据库
CREATE database bantd;

# 安装TimeScaledb
CREATE EXTENSION IF NOT EXISTS timescaledb;

# 查看TimeScaledb的版本，至少是2.13.0
SELECT extversion FROM pg_extension where extname = 'timescaledb';
```
> 如果启动容器时出现错误`OCI runtime create failed: unable to retrieve OCI runtime error: runc did not terminate successfully: exit status 127: unknown.` 是因为`libseccomp`版本过低需要升级：`yum update libseccomp`

# 常见问题
### 重复键违反唯一约束
一般是在从别的数据库同步数据到当前数据库，然后再次插入数据时出现。原因是同步数据后，主键ID序列自增起始值未更新。可通过下面sql更新：
```sql
SELECT setval('tdsignal_id_seq', (SELECT max(id) FROM tdsignal));
```
### 使用超表还是连续聚合
全部使用超表，自行在插入时更新依赖表。因连续聚合无法按sid刷新，在按sid批量插入历史数据后刷新时性能较差
