-- +migrate Up
CREATE TABLE users (
    id INT NOT NULL PRIMARY KEY,
    email NVARCHAR(255) NOT NULL UNIQUE,
    name NVARCHAR(100) NOT NULL,
    created_at DATETIME2(6) NOT NULL DEFAULT SYSUTCDATETIME()
);

-- +migrate Down
DROP TABLE users;
