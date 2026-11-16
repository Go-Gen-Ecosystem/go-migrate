-- +migrate Up
CREATE TABLE posts (
    id INT NOT NULL PRIMARY KEY,
    user_id INT NOT NULL REFERENCES users(id),
    title NVARCHAR(200) NOT NULL
);

-- +migrate Down
DROP VIEW IF EXISTS user_post_counts;
DROP TABLE posts;
