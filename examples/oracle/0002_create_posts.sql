-- +migrate Up
CREATE TABLE posts (
    id INTEGER NOT NULL PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id),
    title VARCHAR2(200 CHAR) NOT NULL
);

-- +migrate Down
DROP VIEW IF EXISTS user_post_counts;
DROP TABLE posts;
