
-- name: GetTask :one
select * from bottask
where id = $1;

-- name: FindTask :one
select * from bottask
where mode = $1 and name = $2
order by create_at desc
limit 1;

-- name: AddTask :one
insert into bottask
("mode", "name", "create_at", "start_at", "stop_at", "info")
values ($1, $2, $3, $4, $5, $6)
returning *;

-- name: ListTasks :many
select * from bottask
order by id;




-- name: ListSymbols :many
select * from exsymbol
where exchange = $1
and market = $2
order by id;

-- name: AddSymbols :copyfrom
insert into exsymbol
(exchange, market, symbol)
values ($1, $2, $3);



-- name: ListTaskPairs :many
select symbol from iorder
where task_id = $1
and enter_at >= $2
and enter_at <= $3;


-- name: ListKInfos :many
select * from kinfo;

-- name: GetKRange :one
select start, stop from kinfo
where sid = $1
and timeframe = $2
limit 1;

-- name: ListKHoles :many
select * from khole;


-- name: GetIOrder :one
select * from iorder
where id = $1;

-- name: GetExOrders :many
select * from exorder
where inout_id=$1;

