-- +migrate Up
ALTER TABLE users ADD COLUMN active BOOLEAN NOT NULL DEFAULT TRUE;

-- +migrate Down
ALTER TABLE users DROP COLUMN active;
