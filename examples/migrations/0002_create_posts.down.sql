-- Repeatable migrations have no down section: drop objects that depend on
-- this table (the R__ view) before dropping it.
DROP VIEW IF EXISTS user_post_counts;
DROP TABLE posts;
