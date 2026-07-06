# GoMigrate CLI Documentation

**GoMigrate** is a powerful, standalone database migration tool for Go applications that supports both sequential versioned migrations and declarative changeset migrations.

## Table of Contents

- [Installation](#installation)
- [Quick Start](#quick-start)
- [Migration Types](#migration-types)
- [Commands Reference](#commands-reference)
- [Configuration](#configuration)
- [Examples](#examples)
- [Best Practices](#best-practices)
- [Troubleshooting](#troubleshooting)

## Installation

### Binary Installation

Download the latest binary from the releases page:

```bash
# Linux/macOS
curl -L https://github.com/your-org/gomigrate/releases/latest/download/gomigrate-linux-amd64 -o gomigrate
chmod +x gomigrate
sudo mv gomigrate /usr/local/bin/

# Windows
# Download gomigrate-windows-amd64.exe and add to PATH
```

### Go Install

```bash
go install github.com/your-org/gomigrate/cmd/gomigrate@latest
```

### From Source

```bash
git clone https://github.com/your-org/gomigrate.git
cd gomigrate
make build
```

## Quick Start

### 1. Initialize a New Project

```bash
gomigrate init --database-url="postgres://user:pass@localhost/mydb"
```

This creates:
- `gomigrate.yaml` - Configuration file
- `migrations/` - Directory for migration files
- Example migration files

### 2. Create Your First Migration

#### Versioned Migration (SQL)
```bash
gomigrate create --type=versioned --format=sql --name="create_users_table"
```

#### Changeset Migration (XML)
```bash
gomigrate create --type=changeset --format=xml --name="create_posts_table"
```

### 3. Run Migrations

```bash
gomigrate up
```

### 4. Check Status

```bash
gomigrate status
```

## Migration Types

### Versioned Migrations

Versioned migrations follow a sequential numbering scheme and are executed in order.

**Naming Convention:**
- `V{version}__{description}.{extension}`
- Example: `V1.0.0__create_users_table.sql`

**Supported Formats:**
- **SQL** (`.sql`) - Direct SQL statements
- **XML** (`.xml`) - GoMigrate XML format
- **YAML** (`.yaml`, `.yml`) - GoMigrate YAML format
- **JSON** (`.json`) - GoMigrate JSON format

### Changeset Migrations

Changeset migrations use unique IDs and can be executed in any order, with dependency resolution.

**Features:**
- Unique changeset IDs
- Dependency resolution
- Preconditions
- Automatic rollback generation
- Cross-format support

## Commands Reference

### Core Commands

#### `init` - Initialize Project

```bash
gomigrate init [flags]
```

**Flags:**
- `--database-url` - Database connection URL
- `--driver` - Database driver (postgres, mysql, sqlite, sqlserver)
- `--migrations-dir` - Migrations directory (default: "migrations")
- `--config-file` - Configuration file path (default: "gomigrate.yaml")

**Examples:**
```bash
# PostgreSQL
gomigrate init --database-url="postgres://user:pass@localhost:5432/mydb?sslmode=disable"

# MySQL
gomigrate init --database-url="mysql://user:pass@localhost:3306/mydb"

# SQLite
gomigrate init --database-url="sqlite://./database.db"

# SQL Server
gomigrate init --database-url="sqlserver://user:pass@localhost:1433?database=mydb"
```

#### `create` - Create New Migration

```bash
gomigrate create [flags]
```

**Flags:**
- `--type` - Migration type: `versioned` or `changeset`
- `--format` - File format: `sql`, `xml`, `yaml`, `json`
- `--name` - Migration name/description
- `--author` - Author name (optional)
- `--template` - Template to use (optional)

**Examples:**
```bash
# Versioned SQL migration
gomigrate create --type=versioned --format=sql --name="add_user_profiles"

# Changeset XML migration
gomigrate create --type=changeset --format=xml --name="create_posts_table" --author="john.doe"

# Changeset YAML migration with template
gomigrate create --type=changeset --format=yaml --name="add_indexes" --template="index_template"
```

#### `up` - Run Migrations

```bash
gomigrate up [flags]
```

**Flags:**
- `--target` - Target version/changeset to migrate to
- `--count` - Number of migrations to run
- `--dry-run` - Show what would be executed without running
- `--force` - Force execution even with warnings

**Examples:**
```bash
# Run all pending migrations
gomigrate up

# Run specific number of migrations
gomigrate up --count=3

# Migrate to specific version
gomigrate up --target="2.1.0"

# Dry run to see what would be executed
gomigrate up --dry-run
```

#### `down` - Rollback Migrations

```bash
gomigrate down [flags]
```

**Flags:**
- `--target` - Target version to rollback to
- `--count` - Number of migrations to rollback
- `--dry-run` - Show what would be executed without running
- `--force` - Force rollback even with warnings

**Examples:**
```bash
# Rollback one migration
gomigrate down --count=1

# Rollback to specific version
gomigrate down --target="1.5.0"

# Dry run rollback
gomigrate down --dry-run --count=2
```

#### `status` - Show Migration Status

```bash
gomigrate status [flags]
```

**Flags:**
- `--format` - Output format: `table`, `json`, `yaml`
- `--verbose` - Show detailed information

**Examples:**
```bash
# Show status in table format
gomigrate status

# Show detailed status
gomigrate status --verbose

# Export status as JSON
gomigrate status --format=json
```

### Advanced Commands

#### `validate` - Validate Migrations

```bash
gomigrate validate [flags]
```

**Flags:**
- `--check-syntax` - Check SQL syntax
- `--check-dependencies` - Validate dependencies
- `--check-checksums` - Verify checksums

**Examples:**
```bash
# Validate all migrations
gomigrate validate

# Check only syntax
gomigrate validate --check-syntax

# Full validation
gomigrate validate --check-syntax --check-dependencies --check-checksums
```

#### `info` - Show Migration Information

```bash
gomigrate info [migration] [flags]
```

**Examples:**
```bash
# Show info for specific migration
gomigrate info V1.0.0__create_users_table.sql

# Show info for changeset
gomigrate info changeset:create_posts:john.doe
```

#### `reset` - Reset Migration State

```bash
gomigrate reset [flags]
```

**Flags:**
- `--hard` - Drop all tables and reset completely
- `--soft` - Reset migration tracking only

**Examples:**
```bash
# Soft reset (reset tracking only)
gomigrate reset

# Hard reset (drop all tables)
gomigrate reset --hard
```

#### `repair` - Repair Migration State

```bash
gomigrate repair [flags]
```

**Flags:**
- `--version` - Mark specific version as applied
- `--remove` - Remove specific version from tracking

**Examples:**
```bash
# Mark version as applied
gomigrate repair --version="1.2.0"

# Remove version from tracking
gomigrate repair --remove --version="1.2.0"
```

#### `baseline` - Set Baseline Version

```bash
gomigrate baseline [flags]
```

**Flags:**
- `--version` - Baseline version
- `--description` - Baseline description

**Examples:**
```bash
# Set baseline to version 1.0.0
gomigrate baseline --version="1.0.0" --description="Initial baseline"
```

### Configuration Commands

#### `config` - Manage Configuration

```bash
gomigrate config [subcommand] [flags]
```

**Subcommands:**
- `show` - Show current configuration
- `set` - Set configuration value
- `get` - Get configuration value
- `validate` - Validate configuration

**Examples:**
```bash
# Show current configuration
gomigrate config show

# Set configuration value
gomigrate config set database.timeout 30s

# Get configuration value
gomigrate config get migrations.auto_rollback

# Validate configuration
gomigrate config validate
```

### Plugin Commands

#### `plugin` - Manage Plugins

```bash
gomigrate plugin [subcommand] [flags]
```

**Subcommands:**
- `list` - List installed plugins
- `install` - Install plugin
- `remove` - Remove plugin
- `info` - Show plugin information

**Examples:**
```bash
# List installed plugins
gomigrate plugin list

# Install plugin
gomigrate plugin install github.com/example/gomigrate-plugin

# Remove plugin
gomigrate plugin remove example-plugin

# Show plugin info
gomigrate plugin info example-plugin
```

## Configuration

### Configuration File (gomigrate.yaml)

```yaml
# Database configuration
database:
  url: "postgres://user:pass@localhost:5432/mydb"
  driver: "postgres"
  max_connections: 10
  timeout: "30s"
  ssl_mode: "disable"

# Migration settings
migrations:
  directory: "migrations"
  table_name: "schema_migrations"
  lock_table: "schema_migrations_lock"
  lock_timeout: "15m"
  auto_rollback: true
  validate_on_migrate: true
  baseline_on_migrate: false
  baseline_version: "1.0.0"

# Feature flags
features:
  auto_rollback: true
  backup_before_migrate: true
  validate_migrations: true
  dependency_resolution: true
  preconditions: true
  changelog: true

# Logging configuration
logging:
  level: "info"
  format: "text"
  output: "stdout"
  file: ""

# Plugin configuration
plugins:
  directory: "plugins"
  enabled: []

# Environment-specific overrides
environments:
  development:
    database:
      url: "postgres://dev:dev@localhost:5432/mydb_dev"
    logging:
      level: "debug"
  
  production:
    database:
      url: "${DATABASE_URL}"
    logging:
      level: "warn"
      file: "/var/log/gomigrate.log"
    features:
      backup_before_migrate: true
```

### Environment Variables

```bash
# Database
GOMIGRATE_DATABASE_URL="postgres://user:pass@localhost:5432/mydb"
GOMIGRATE_DATABASE_DRIVER="postgres"

# Migrations
GOMIGRATE_MIGRATIONS_DIR="migrations"
GOMIGRATE_MIGRATIONS_TABLE="schema_migrations"

# Features
GOMIGRATE_AUTO_ROLLBACK="true"
GOMIGRATE_VALIDATE_ON_MIGRATE="true"

# Logging
GOMIGRATE_LOG_LEVEL="info"
GOMIGRATE_LOG_FORMAT="json"
```

## Examples

### Example 1: Basic Versioned Migration

**Create migration:**
```bash
gomigrate create --type=versioned --format=sql --name="create_users_table"
```

**Generated file: `V1.0.0__create_users_table.sql`**
```sql
-- Create users table
CREATE TABLE users (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    username VARCHAR(255) NOT NULL UNIQUE,
    email VARCHAR(255) NOT NULL UNIQUE,
    password_hash VARCHAR(255) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
);

CREATE INDEX idx_users_username ON users(username);
CREATE INDEX idx_users_email ON users(email);
```

### Example 2: Changeset Migration with Dependencies

**Create migration:**
```bash
gomigrate create --type=changeset --format=xml --name="create_posts_table"
```

**Generated file: `changeset_posts.xml`**
```xml
<?xml version="1.0" encoding="UTF-8"?>
<databaseChangeLog xmlns="http://www.gomigrate.org/xml/ns/dbchangelog">
    <changeSet id="1" author="john.doe">
        <preConditions onFail="MARK_RAN">
            <tableExists tableName="users"/>
        </preConditions>
        
        <createTable tableName="posts">
            <column name="id" type="BIGINT" autoIncrement="true">
                <constraints primaryKey="true" nullable="false"/>
            </column>
            <column name="user_id" type="BIGINT">
                <constraints nullable="false"/>
            </column>
            <column name="title" type="VARCHAR(500)">
                <constraints nullable="false"/>
            </column>
            <column name="content" type="TEXT">
                <constraints nullable="false"/>
            </column>
        </createTable>
        
        <addForeignKeyConstraint
            baseTableName="posts"
            baseColumnNames="user_id"
            constraintName="fk_posts_user"
            referencedTableName="users"
            referencedColumnNames="id"/>
    </changeSet>
</databaseChangeLog>
```

### Example 3: YAML Changeset with Rollback

**File: `changeset_comments.yaml`**
```yaml
databaseChangeLog:
  - changeSet:
      id: 1
      author: jane.doe
      comment: Create comments table
      changes:
        - createTable:
            tableName: comments
            columns:
              - column:
                  name: id
                  type: BIGINT
                  autoIncrement: true
                  constraints:
                    primaryKey: true
              - column:
                  name: post_id
                  type: BIGINT
                  constraints:
                    nullable: false
              - column:
                  name: content
                  type: TEXT
                  constraints:
                    nullable: false
      rollback:
        - dropTable:
            tableName: comments
```

## Best Practices

### 1. Migration Naming

**Versioned Migrations:**
- Use semantic versioning: `V1.0.0`, `V1.1.0`, `V2.0.0`
- Include descriptive names: `V1.0.0__create_users_table.sql`
- Use consistent naming conventions

**Changeset Migrations:**
- Use descriptive changeset IDs: `create_users_table`, `add_user_indexes`
- Include author information
- Group related changes in single changesets

### 2. Migration Structure

- **One change per migration** for versioned migrations
- **Related changes in one changeset** for changeset migrations
- Always include rollback instructions
- Add comments explaining complex changes

### 3. Testing

```bash
# Always validate before applying
gomigrate validate

# Use dry-run to preview changes
gomigrate up --dry-run

# Test rollbacks
gomigrate down --dry-run --count=1
```

### 4. Production Deployment

```bash
# Backup before migration
gomigrate backup

# Run with validation
gomigrate up --validate

# Monitor status
gomigrate status --verbose
```

### 5. Version Control

- Commit migration files with application code
- Never modify applied migrations
- Use feature branches for migration development
- Review migrations in pull requests

## Troubleshooting

### Common Issues

#### 1. Migration Fails

```bash
# Check migration status
gomigrate status

# Validate migrations
gomigrate validate

# Check logs
gomigrate info [migration-name]
```

#### 2. Lock Timeout

```bash
# Check for stuck locks
gomigrate status --verbose

# Force unlock (use with caution)
gomigrate repair --unlock
```

#### 3. Checksum Mismatch

```bash
# Repair checksum
gomigrate repair --version="1.2.0"

# Reset and reapply
gomigrate reset --soft
gomigrate up
```

#### 4. Dependency Issues

```bash
# Validate dependencies
gomigrate validate --check-dependencies

# Show dependency graph
gomigrate info --dependencies
```

### Debug Mode

```bash
# Enable debug logging
export GOMIGRATE_LOG_LEVEL=debug
gomigrate up

# Or use config
gomigrate --log-level=debug up
```

### Getting Help

```bash
# General help
gomigrate help

# Command-specific help
gomigrate help up
gomigrate help create

# Show version
gomigrate version
```

## Advanced Features

### 1. Custom Templates

Create custom migration templates in `templates/` directory:

```bash
# Use custom template
gomigrate create --template=my_template --name="custom_migration"
```

### 2. Plugins

Extend functionality with plugins:

```bash
# Install backup plugin
gomigrate plugin install github.com/example/gomigrate-backup

# Use plugin
gomigrate backup create --name="pre_migration_backup"
```

### 3. Multiple Environments

```bash
# Use environment-specific config
gomigrate --env=production up

# Override with environment variables
GOMIGRATE_ENV=staging gomigrate up
```

### 4. CI/CD Integration

```yaml
# GitHub Actions example
- name: Run Migrations
  run: |
    gomigrate validate
    gomigrate up --dry-run
    gomigrate up
```

For more information, visit the [official documentation](https://github.com/your-org/gomigrate) or join our [community discussions](https://github.com/your-org/gomigrate/discussions).
