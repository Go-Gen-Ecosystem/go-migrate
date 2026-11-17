-- +migrate Up
CREATE INDEX users_name_idx ON users (name);

-- +migrate Down
DROP INDEX users_name_idx;
