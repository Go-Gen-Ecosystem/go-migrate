-- +migrate Up
CREATE TABLE posts (
    id INTEGER NOT NULL PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id),
    title VARCHAR(200) NOT NULL
);

-- +migrate Down
DROP VIEW user_post_counts;
DROP TABLE posts;
