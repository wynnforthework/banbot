
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



-- name: ListExchanges :many
select distinct exchange from exsymbol;

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


-- name: ListKHoles :many
select * from khole
order by sid, start;

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

-- name: AddIOrder :one
insert into iorder ("task_id", "symbol", "sid", "timeframe", "short", "status",
                    "enter_tag", "init_price", "quote_cost", "exit_tag", "leverage",
                    "enter_at", "exit_at", "strategy", "stg_ver", "profit_rate", "profit", "info")
values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
    RETURNING id;

-- name: SetIOrder :exec
update iorder set
      "task_id" = $1,
      "symbol" = $2,
      "sid" = $3,
      "timeframe" = $4,
      "short" = $5,
      "status" = $6,
      "enter_tag" = $7,
      "init_price" = $8,
      "quote_cost" = $9,
      "exit_tag" = $10,
      "leverage" = $11,
      "enter_at" = $12,
      "exit_at" = $13,
      "strategy" = $14,
      "stg_ver" = $15,
      "profit_rate" = $16,
      "profit" = $17,
      "info" = $18
    WHERE id = $19;

-- name: AddExOrder :one
insert into exorder ("task_id", "inout_id", "symbol", "enter", "order_type", "order_id", "side",
                     "create_at", "price", "average", "amount", "filled", "status", "fee", "fee_type", "update_at")
values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
    RETURNING id;

-- name: SetExOrder :exec
update exorder set
       "task_id" = $1,
       "inout_id" = $2,
       "symbol" = $3,
       "enter" = $4,
       "order_type" = $5,
       "order_id" = $6,
       "side" = $7,
       "create_at" = $8,
       "price" = $9,
       "average" = $10,
       "amount" = $11,
       "filled" = $12,
       "status" = $13,
       "fee" = $14,
       "fee_type" = $15,
       "update_at" = $16
    where id = $17;
