
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
order by id;

-- name: AddSymbols :copyfrom
insert into exsymbol
(exchange, market, symbol)
values ($1, $2, $3);

-- name: SetListMS :exec
update exsymbol set list_ms = $2, delist_ms = $3
where id = $1;


-- name: ListTaskPairs :many
select symbol from iorder
where task_id = $1
and enter_at >= $2
and enter_at <= $3;


-- name: ListKInfos :many
select * from kinfo;

-- name: SetKInfo :exec
update kinfo set start = $3, stop = $4
where sid = $1 and timeframe = $2;

-- name: AddKInfo :one
insert into kinfo
("sid", "timeframe", "start", "stop")
values ($1, $2, $3, $4)
    returning *;



-- name: GetKHoles :many
select * from khole
where sid = $1 and timeframe = $2;

-- name: SetKHole :exec
update khole set start = $2, stop = $3
where id = $1;

-- name: DelKHoleRange :exec
delete from khole
where sid = $1 and timeframe=$2 and start >= $3 and stop <= $4;

-- name: AddKHoles :copyfrom
insert into khole
("sid", "timeframe", "start", "stop")
values ($1, $2, $3, $4);




-- name: GetIOrder :one
select * from iorder
where id = $1;

-- name: GetExOrders :many
select * from exorder
where inout_id=$1;

