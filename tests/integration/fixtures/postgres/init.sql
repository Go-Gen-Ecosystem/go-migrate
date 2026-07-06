-- PostgreSQL initialization script for integration tests
-- This script runs when the PostgreSQL container starts

-- Ensure the test database exists
CREATE DATABASE IF NOT EXISTS migrations_test;

-- Grant permissions to test user
GRANT ALL PRIVILEGES ON DATABASE migrations_test TO test_user;

-- Switch to test database
\c migrations_test;

-- Grant schema permissions
GRANT ALL ON SCHEMA public TO test_user;
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO test_user;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO test_user;

-- Set default privileges for future objects
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO test_user;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON SEQUENCES TO test_user;