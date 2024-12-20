DROP TABLE IF EXISTS task;
CREATE TABLE task
(
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    mode      TEXT    NOT NULL,
    args      TEXT    NOT NULL,
    config    TEXT    NOT NULL,
    path      TEXT    NOT NULL,
    create_at INTEGER NOT NULL,
    start_at  INTEGER NOT NULL,
    stop_at   INTEGER NOT NULL,
    info      TEXT    NOT NULL
);
