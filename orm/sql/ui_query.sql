
-- name: AddTask :one
insert into task
("mode", "args", "config", "create_at", "start_at", "stop_at", "info")
values (?, ?, ?, ?, ?, ?, ?)
    returning *;

-- name: ListTasks :many
select * from task
where mode = ? and id < ?
order by id desc
limit ?;

-- name: GetTask :one
select * from task
where id = ?;
