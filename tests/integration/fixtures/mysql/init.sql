-- MySQL initialization script for integration tests
-- This script runs when the MySQL container starts

-- Ensure the test database exists
CREATE DATABASE IF NOT EXISTS migrations_test;

-- Grant permissions to test user
GRANT ALL PRIVILEGES ON migrations_test.* TO 'test_user'@'%';
FLUSH PRIVILEGES;

-- Use the test database
USE migrations_test;

-- Create a test table to verify connectivity (will be cleaned up by tests)
CREATE TABLE IF NOT EXISTS _connectivity_test (
    id INT AUTO_INCREMENT PRIMARY KEY,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Insert a test record
INSERT INTO _connectivity_test VALUES (1, NOW());

-- Clean up the test table
DROP TABLE _connectivity_test;