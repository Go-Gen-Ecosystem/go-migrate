CREATE TABLE posts (
    id      INTEGER      NOT NULL PRIMARY KEY,
    user_id INTEGER      NOT NULL,
    title   VARCHAR(200) NOT NULL,
    body    TEXT,
    CONSTRAINT fk_posts_user FOREIGN KEY (user_id) REFERENCES users (id)
);
