-- 这里存放不使用sqlc自动生成的表结构

-- ----------------------------
-- Table structure for kline_1m
-- ----------------------------
DROP TABLE IF EXISTS "public"."kline_1m";
CREATE TABLE "public"."kline_1m"
(
    "sid"    int4 NOT NULL,
    "time"   int8      not null,
    "open"   float8    not null,
    "high"   float8    not null,
    "low"    float8    not null,
    "close"  float8    not null,
    "volume" float8    not null,
    "info"   float8    not null
);
CREATE INDEX "kline_1m_sid" ON "public"."kline_1m" USING btree ("sid");
CREATE UNIQUE INDEX "kline_1m_sid_time" ON "public"."kline_1m" ("sid", "time");
SELECT create_hypertable('kline_1m', by_range('time', 5184000000));
ALTER TABLE "public"."kline_1m"
    SET (
        timescaledb.compress,
        timescaledb.compress_orderby = 'time DESC',
        timescaledb.compress_segmentby = 'sid'
        );
SELECT add_compression_policy('kline_1m', 5184000000);

-- ----------------------------
-- Table structure for kline_5m
-- ----------------------------
DROP TABLE IF EXISTS "public"."kline_5m";
CREATE TABLE "public"."kline_5m"
(
    "sid"    int4 NOT NULL,
    "time"   int8      not null,
    "open"   float8    not null,
    "high"   float8    not null,
    "low"    float8    not null,
    "close"  float8    not null,
    "volume" float8    not null,
    "info"   float8    not null
);
CREATE INDEX "kline_5m_sid" ON "public"."kline_5m" USING btree ("sid");
CREATE UNIQUE INDEX "kline_5m_sid_time" ON "public"."kline_5m" ("sid", "time");
SELECT create_hypertable('kline_5m', by_range('time', 7776000000));
ALTER TABLE "public"."kline_5m"
    SET (
        timescaledb.compress,
        timescaledb.compress_orderby = 'time DESC',
        timescaledb.compress_segmentby = 'sid'
        );
SELECT add_compression_policy('kline_5m', 5184000000);

-- ----------------------------
-- Table structure for kline_15m
-- ----------------------------
DROP TABLE IF EXISTS "public"."kline_15m";
CREATE TABLE "public"."kline_15m"
(
    "sid"    int4 NOT NULL,
    "time"   int8      not null,
    "open"   float8    not null,
    "high"   float8    not null,
    "low"    float8    not null,
    "close"  float8    not null,
    "volume" float8    not null,
    "info"   float8    not null
);
CREATE INDEX "kline_15m_sid" ON "public"."kline_15m" USING btree ("sid");
CREATE UNIQUE INDEX "kline_15m_sid_time" ON "public"."kline_15m" ("sid", "time");
SELECT create_hypertable('kline_15m', by_range('time', 31536000000));
ALTER TABLE "public"."kline_15m"
    SET (
        timescaledb.compress,
        timescaledb.compress_orderby = 'time DESC',
        timescaledb.compress_segmentby = 'sid'
        );
SELECT add_compression_policy('kline_15m', 7776000000);

-- ----------------------------
-- Table structure for kline_1h
-- ----------------------------
DROP TABLE IF EXISTS "public"."kline_1h";
CREATE TABLE "public"."kline_1h"
(
    "sid"    int4 NOT NULL,
    "time"   int8      not null,
    "open"   float8    not null,
    "high"   float8    not null,
    "low"    float8    not null,
    "close"  float8    not null,
    "volume" float8    not null,
    "info"   float8    not null
);
CREATE INDEX "kline_1h_sid" ON "public"."kline_1h" USING btree ("sid");
CREATE UNIQUE INDEX "kline_1h_sid_time" ON "public"."kline_1h" ("sid", "time");
SELECT create_hypertable('kline_1h', by_range('time', 63072000000));
ALTER TABLE "public"."kline_1h"
    SET (
        timescaledb.compress,
        timescaledb.compress_orderby = 'time DESC',
        timescaledb.compress_segmentby = 'sid'
        );
SELECT add_compression_policy('kline_1h', 15552000000);

-- ----------------------------
-- Table structure for kline_1d
-- ----------------------------
DROP TABLE IF EXISTS "public"."kline_1d";
CREATE TABLE "public"."kline_1d"
(
    "sid"    int4 NOT NULL,
    "time"   int8      not null,
    "open"   float8    not null,
    "high"   float8    not null,
    "low"    float8    not null,
    "close"  float8    not null,
    "volume" float8    not null,
    "info"   float8    not null
);
CREATE INDEX "kline_1d_sid" ON "public"."kline_1d" USING btree ("sid");
CREATE UNIQUE INDEX "kline_1d_sid_time" ON "public"."kline_1d" ("sid", "time");
SELECT create_hypertable('kline_1d', by_range('time', 94608000000));
ALTER TABLE "public"."kline_1d"
    SET (
        timescaledb.compress,
        timescaledb.compress_orderby = 'time DESC',
        timescaledb.compress_segmentby = 'sid'
        );
SELECT add_compression_policy('kline_1d', 94608000000);
