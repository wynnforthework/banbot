-- name: AddTask :one
insert into task
(mode, args, config, path, strats, periods, pairs, create_at, start_at, stop_at, status, progress, order_num, profit_rate, win_rate, max_drawdown, sharpe, info, note)
values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    returning *;


-- name: DelTasks :exec
delete from task where id in (sqlc.slice('ids'));

-- name: UpdateTask :exec
update task set status=?,progress=?,order_num=?,profit_rate=?,win_rate=?,max_drawdown=?,sharpe=?,info=?  where id = ?;

-- name: SetTaskNote :exec
update task set note=? where id = ?;

-- name: SetTaskPath :exec
update task set path=? where id = ?;

-- name: GetTask :one
select * from task
where id = ?;

-- name: GetTaskOptions :many
select strats, periods, start_at, stop_at from task;

