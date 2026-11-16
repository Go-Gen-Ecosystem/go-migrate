-- Portable across PostgreSQL, MySQL and SQLite.
-- +migrate Up
CREATE TABLE users (
    id         INTEGER      NOT NULL PRIMARY KEY,
    email      VARCHAR(255) NOT NULL UNIQUE,
    name       VARCHAR(100) NOT NULL,
    created_at TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +migrate Down
DROP TABLE users;
