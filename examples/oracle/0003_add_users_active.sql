-- +migrate Up
ALTER TABLE users ADD active NUMBER(1) DEFAULT 1 NOT NULL;

-- +migrate Down
ALTER TABLE users DROP COLUMN active;
