CREATE EXTENSION IF NOT EXISTS timescaledb;

-- ----------------------------
-- Table structure for khole
-- ----------------------------
DROP TABLE IF EXISTS "public"."khole";
CREATE TABLE "public"."khole"
(
    "id"        BIGSERIAL  NOT NULL PRIMARY KEY,
    "sid"       int4       not null,
    "timeframe" varchar(5) not null,
    "start"     int8       not null,
    "stop"      int8       not null,
    "no_data"   boolean    not null DEFAULT FALSE
);
CREATE INDEX "idx_khole_sid_tf" ON "public"."khole" USING btree ("sid", "timeframe");
ALTER TABLE "public"."khole"
    ADD CONSTRAINT "idx_khole_sid_tf_start" UNIQUE ("sid", "timeframe", "start");

-- ----------------------------
-- Table structure for kinfo
-- ----------------------------
DROP TABLE IF EXISTS "public"."kinfo";
CREATE TABLE "public"."kinfo"
(
    "sid"       int4       NOT NULL ,
    "timeframe" varchar(5) NOT NULL,
    "start"     int8       not null,
    "stop"      int8       not null
);
CREATE Unique INDEX "idx_kinfo_sid_tf" ON "public"."kinfo" USING btree ("sid", "timeframe");

-- ----------------------------
-- Table structure for kline_un
-- ----------------------------
DROP TABLE IF EXISTS "public"."kline_un";
CREATE TABLE "public"."kline_un"
(
    "sid"       int4       NOT NULL ,
    "start_ms"  int8       NOT NULL,
    "stop_ms"   int8       NOT NULL,
    "timeframe" varchar(5) NOT NULL,
    "open"      float8     not null,
    "high"      float8     not null,
    "low"       float8     not null,
    "close"     float8     not null,
    "volume"    float8     not null,
    "info"      float8     not null
);
CREATE unique INDEX "kline_un_sid_tf_idx" ON "public"."kline_un" USING btree ("sid", "timeframe");

-- ----------------------------
-- Table structure for symbol
-- ----------------------------
DROP TABLE IF EXISTS "public"."exsymbol";
CREATE TABLE "public"."exsymbol"
(
    "id"        SERIAL   NOT NULL PRIMARY KEY,
    "exchange"  varchar(50) not null,
    "exg_real"  varchar(50) not null,
    "market"    varchar(20) not null,
    "symbol"    varchar(50) not null,
    "combined"  boolean     not null default false,
    "list_ms"   int8      default 0  not null,
    "delist_ms" int8      default 0  not null
);
CREATE UNIQUE INDEX "ix_exsymbol_unique" ON "public"."exsymbol" ("exchange", "market", "symbol");


-- ----------------------------
-- Table structure for calendars
-- ----------------------------
DROP TABLE IF EXISTS "public"."calendars";
CREATE TABLE "public"."calendars"
(
    "id"              SERIAL    NOT NULL PRIMARY KEY,
    "name"  varchar(50) not null,
    "start_ms"          int8 not null,
    "stop_ms"          int8  not null
);
CREATE INDEX "idx_calendar_name" ON "public"."calendars" USING btree ("name");


-- ----------------------------
-- Table structure for adj_factors
-- ----------------------------
DROP TABLE IF EXISTS "public"."adj_factors";
CREATE TABLE "public"."adj_factors"
(
    "id"            SERIAL    NOT NULL PRIMARY KEY,
    "sid"           int4        not null,
    "sub_id"        int4        not null,
    "start_ms"      int8        not null,
    "factor"        float8      not null
);
CREATE INDEX "idx_adj_factors_sid" ON "public"."adj_factors" USING btree ("sid");
CREATE INDEX "idx_adj_factors_start" ON "public"."adj_factors" USING btree ("start_ms");


-- ----------------------------
-- Table structure for calendars
-- ----------------------------
DROP TABLE IF EXISTS "public"."ins_kline";
CREATE TABLE "public"."ins_kline"
(
    "id"              SERIAL    NOT NULL PRIMARY KEY,
    "sid"           int4        not null,
    "timeframe"   varchar(5)    not null,
    "start_ms"          int8 not null,
    "stop_ms"          int8  not null
);
CREATE INDEX "idx_ins_kline_sid" ON "public"."ins_kline" USING btree ("sid");

