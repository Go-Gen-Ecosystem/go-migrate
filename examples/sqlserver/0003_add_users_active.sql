-- +migrate Up
ALTER TABLE users ADD active BIT NOT NULL CONSTRAINT df_users_active DEFAULT 1;

-- +migrate Down
ALTER TABLE users DROP CONSTRAINT df_users_active;
GO
ALTER TABLE users DROP COLUMN active;
