-- Repeatable: re-applied after versioned migrations whenever this file changes.
DROP VIEW IF EXISTS user_post_counts;
CREATE VIEW user_post_counts AS
SELECT u.id, u.email, COUNT(p.id) AS posts
FROM users u
LEFT JOIN posts p ON p.user_id = u.id
GROUP BY u.id, u.email;
