

-- name: ListExchanges :many
select distinct exchange from exsymbol;

-- name: ListSymbols :many
select * from exsymbol
where exchange = $1
order by id;

-- name: AddSymbols :copyfrom
insert into exsymbol
(exchange, exg_real, market, symbol)
values ($1, $2, $3, $4);

-- name: SetListMS :exec
update exsymbol set list_ms = $2, delist_ms = $3
where id = $1;



-- name: ListKInfos :many
select * from kinfo;

-- name: FindKInfos :many
select * from kinfo
where sid = $1;

-- name: SetKInfo :exec
update kinfo set start = $3, stop = $4
where sid = $1 and timeframe = $2;

-- name: AddKInfo :one
insert into kinfo
("sid", "timeframe", "start", "stop")
values ($1, $2, $3, $4)
    returning *;



-- name: ListKHoles :many
select * from khole
WHERE sid = ANY($1::int[]);

-- name: GetKHoles :many
select * from khole
where sid = $1 and timeframe = $2 and start >= $3 and stop <= $4;

-- name: SetKHole :exec
update khole set start = $2, stop = $3, no_data = $4
where id = $1;

-- name: DelKHoleRange :exec
delete from khole
where sid = $1 and timeframe=$2 and start >= $3 and stop <= $4;

-- name: AddKHoles :copyfrom
insert into khole
("sid", "timeframe", "start", "stop", "no_data")
values ($1, $2, $3, $4, $5);



-- name: AddCalendars :copyfrom
insert into calendars
(name, start_ms, stop_ms)
values ($1, $2, $3);



-- name: AddAdjFactors :copyfrom
insert into adj_factors
(sid, sub_id, start_ms, factor)
values ($1, $2, $3, $4);

-- name: GetAdjFactors :many
select * from adj_factors
where sid=$1
order by start_ms;

-- name: DelAdjFactors :exec
delete from adj_factors
where sid=$1;



-- name: GetInsKline :one
select * from ins_kline
where sid=$1;

-- name: GetAllInsKlines :many
select * from ins_kline;

-- name: DelInsKline :exec
delete from ins_kline
where id=$1;

-- name: AddInsKline :one
insert into ins_kline ("sid", "timeframe", "start_ms", "stop_ms")
values ($1, $2, $3, $4) RETURNING id;
