# GoMigrate - Advanced Database Migration Tool

[![Go Version](https://img.shields.io/badge/Go-1.21+-blue.svg)](https://golang.org)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Build Status](https://img.shields.io/badge/Build-Passing-brightgreen.svg)](https://github.com/your-org/gomigrate)

GoMigrate is a powerful, standalone database migration tool for Go applications that provides enterprise-grade database management capabilities. It supports multiple migration formats, automatic rollback generation, dependency resolution, and advanced features for modern database operations.

## 🚀 Features

### Core Features
- **Multiple Migration Types**: Support for both sequential versioned migrations and declarative changeset migrations
- **Multiple Formats**: SQL, XML, YAML, and JSON migration files
- **Automatic Rollback**: Intelligent rollback script generation for all supported formats
- **Dependency Resolution**: Advanced dependency tracking and resolution
- **Database Support**: MySQL, PostgreSQL, SQLite, SQL Server
- **Standalone**: Works independently without requiring the full gogen framework

### Advanced Features
- **Dry Run Mode**: Preview changes before execution
- **Backup & Restore**: Automatic database backups before migrations
- **Validation**: Comprehensive migration validation and syntax checking
- **Locking**: Distributed locking to prevent concurrent migrations
- **Monitoring**: Built-in monitoring and logging capabilities
- **Plugin System**: Extensible architecture with plugin support
- **Configuration Management**: Flexible configuration with environment support

## 📦 Installation

### Binary Installation

Download the latest binary from the [releases page](https://github.com/your-org/gomigrate/releases):

```bash
# Linux/macOS
curl -L https://github.com/your-org/gomigrate/releases/latest/download/gomigrate-linux-amd64.tar.gz | tar xz
sudo mv gomigrate /usr/local/bin/

# Windows (PowerShell)
Invoke-WebRequest -Uri "https://github.com/your-org/gomigrate/releases/latest/download/gomigrate-windows-amd64.zip" -OutFile "gomigrate.zip"
Expand-Archive gomigrate.zip
Move-Item gomigrate\gomigrate.exe C:\Windows\System32\
```

### Build from Source

```bash
git clone https://github.com/your-org/gomigrate.git
cd gomigrate
make build
sudo cp build/gomigrate /usr/local/bin/
```

### Go Install

```bash
go install github.com/your-org/gomigrate/cmd@latest
```

## 🏁 Quick Start

### 1. Initialize a New Project

```bash
# Initialize with SQLite (default)
gomigrate init

# Initialize with MySQL
gomigrate init --driver mysql --database myapp --username user --password pass

# Initialize with PostgreSQL
gomigrate init --driver postgres --database myapp --host localhost --port 5432
```

### 2. Create Your First Migration

```bash
# Create a versioned SQL migration
gomigrate create --type versioned --format sql --name "create_users_table"

# Create a changeset XML migration
gomigrate create --type changeset --format xml --name "add_user_indexes" --author "developer"
```

### 3. Run Migrations

```bash
# Run all pending migrations
gomigrate up

# Run migrations with dry-run
gomigrate up --dry-run

# Migrate to specific version
gomigrate up V2.1.0
```

### 4. Check Status

```bash
# Show migration status
gomigrate status

# Show detailed information
gomigrate info
```

## 📝 Migration Types

### Versioned Migrations

Versioned migrations follow a sequential numbering scheme and are ideal for schema changes:

```sql
-- V1.0.0__create_users_table.sql
CREATE TABLE users (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    username VARCHAR(255) NOT NULL UNIQUE,
    email VARCHAR(255) NOT NULL UNIQUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

### Changeset Migrations

Changeset migrations provide more granular control and are ideal for complex deployments:

#### XML Format
```xml
<?xml version="1.0" encoding="UTF-8"?>
<databaseChangeLog xmlns="http://www.gomigrate.org/xml/ns/dbchangelog">
    <changeSet id="1" author="developer">
        <createTable tableName="posts">
            <column name="id" type="BIGINT" autoIncrement="true">
                <constraints primaryKey="true" nullable="false"/>
            </column>
            <column name="title" type="VARCHAR(255)">
                <constraints nullable="false"/>
            </column>
        </createTable>
        <rollback>
            <dropTable tableName="posts"/>
        </rollback>
    </changeSet>
</databaseChangeLog>
```

#### YAML Format
```yaml
databaseChangeLog:
  - changeSet:
      id: "2"
      author: "developer"
      changes:
        - createIndex:
            tableName: users
            indexName: idx_users_email
            columns:
              - column:
                  name: email
```

#### JSON Format
```json
{
  "databaseChangeLog": [
    {
      "changeSet": {
        "id": "3",
        "author": "developer",
        "changes": [
          {
            "addColumn": {
              "tableName": "users",
              "columns": [
                {
                  "column": {
                    "name": "last_login",
                    "type": "TIMESTAMP"
                  }
                }
              ]
            }
          }
        ]
      }
    }
  ]
}
```

## ⚙️ Configuration

GoMigrate uses a YAML configuration file (`.gomigrate.yaml`):

```yaml
database:
  driver: mysql
  host: localhost
  port: 3306
  database: myapp
  username: user
  password: pass
  ssl_mode: disable
  
migration:
  migrations_path: migrations
  table_name: schema_migrations
  lock_table: schema_migrations_lock
  enable_locking: true
  timeout: 300
  
features:
  auto_rollback: true
  backup_enabled: true
  validation_enabled: true
  dependency_resolution: true
  
logging:
  level: info
  format: text
  output: stdout
  
environments:
  development:
    database:
      database: myapp_dev
  production:
    database:
      database: myapp_prod
      ssl_mode: require
```

## 🔧 Commands

### Core Commands

| Command | Description | Example |
|---------|-------------|---------|
| `init` | Initialize new project | `gomigrate init --driver mysql` |
| `create` | Create new migration | `gomigrate create --name "add_users"` |
| `up` | Run migrations | `gomigrate up` |
| `down` | Rollback migrations | `gomigrate down V1.0.0` |
| `status` | Show migration status | `gomigrate status` |
| `validate` | Validate migrations | `gomigrate validate` |

### Advanced Commands

| Command | Description | Example |
|---------|-------------|---------|
| `info` | Show detailed info | `gomigrate info V1.0.0` |
| `reset` | Reset migration state | `gomigrate reset --force` |
| `repair` | Repair migration state | `gomigrate repair` |
| `baseline` | Baseline existing DB | `gomigrate baseline V1.0.0` |

### Configuration Commands

| Command | Description | Example |
|---------|-------------|---------|
| `config show` | Show current config | `gomigrate config show` |
| `config set` | Set config value | `gomigrate config set database.host localhost` |

### Plugin Commands

| Command | Description | Example |
|---------|-------------|---------|
| `plugin list` | List available plugins | `gomigrate plugin list` |
| `plugin install` | Install plugin | `gomigrate plugin install backup-plugin` |

## 🔌 Plugins

GoMigrate supports a plugin system for extending functionality:

### Available Plugins

- **backup-plugin**: Automatic database backups
- **notification-plugin**: Slack/email notifications
- **monitoring-plugin**: Metrics and monitoring
- **audit-plugin**: Detailed audit logging

### Creating Custom Plugins

```go
package main

import (
    "github.com/your-org/gomigrate/pkg/plugin"
)

type MyPlugin struct{}

func (p *MyPlugin) Name() string {
    return "my-plugin"
}

func (p *MyPlugin) Execute(ctx context.Context, event plugin.Event) error {
    // Plugin logic here
    return nil
}

func main() {
    plugin.Serve(&MyPlugin{})
}
```

## 🧪 Testing

### Running Tests

```bash
# Run all tests
make test

# Run tests with coverage
make test-coverage

# Run benchmarks
make benchmark
```

### Integration Tests

```bash
# Run integration tests with different databases
make test-integration-mysql
make test-integration-postgres
make test-integration-sqlite
```

## 🚀 Deployment

### Docker

```dockerfile
FROM alpine:latest
RUN apk --no-cache add ca-certificates
COPY gomigrate /usr/local/bin/
ENTRYPOINT ["gomigrate"]
```

```bash
# Build Docker image
make docker-build

# Run with Docker
docker run --rm -v $(pwd):/workspace gomigrate:latest status
```

### CI/CD Integration

#### GitHub Actions

```yaml
name: Database Migration
on:
  push:
    branches: [main]

jobs:
  migrate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - name: Run Migrations
        run: |
          curl -L https://github.com/your-org/gomigrate/releases/latest/download/gomigrate-linux-amd64.tar.gz | tar xz
          ./gomigrate up --environment production
```

#### GitLab CI

```yaml
migrate:
  stage: deploy
  script:
    - wget https://github.com/your-org/gomigrate/releases/latest/download/gomigrate-linux-amd64.tar.gz
    - tar xz < gomigrate-linux-amd64.tar.gz
    - ./gomigrate up --environment production
```

## 📊 Monitoring and Observability

### Metrics

GoMigrate exposes Prometheus metrics:

- `gomigrate_migrations_total`: Total number of migrations
- `gomigrate_migration_duration_seconds`: Migration execution time
- `gomigrate_migration_errors_total`: Number of migration errors

### Logging

Structured logging with configurable levels and formats:

```bash
# JSON logging
gomigrate up --log-format json --log-level debug

# Text logging with timestamps
gomigrate up --log-format text --log-level info
```

## 🔒 Security

### Best Practices

1. **Credential Management**: Use environment variables or secure vaults
2. **Network Security**: Enable SSL/TLS for database connections
3. **Access Control**: Implement proper database user permissions
4. **Audit Logging**: Enable comprehensive audit trails

### Security Features

- Encrypted configuration files
- Secure credential storage
- SSL/TLS support for all databases
- Role-based access control

## 🤝 Contributing

We welcome contributions! Please see our [Contributing Guide](CONTRIBUTING.md) for details.

### Development Setup

```bash
# Clone repository
git clone https://github.com/your-org/gomigrate.git
cd gomigrate

# Setup development environment
make dev-setup

# Run tests
make test

# Build
make build
```

### Code Standards

- Follow Go best practices
- Write comprehensive tests
- Document all public APIs
- Use conventional commits

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 🆘 Support

- **Documentation**: [https://gomigrate.dev](https://gomigrate.dev)
- **Issues**: [GitHub Issues](https://github.com/your-org/gomigrate/issues)
- **Discussions**: [GitHub Discussions](https://github.com/your-org/gomigrate/discussions)
- **Slack**: [#gomigrate](https://slack.gomigrate.dev)

## 🗺️ Roadmap

- [ ] GraphQL schema migrations
- [ ] Multi-tenant support
- [ ] Advanced rollback strategies
- [ ] Cloud provider integrations
- [ ] Web UI for migration management
- [ ] Advanced dependency resolution
- [ ] Performance optimizations

## 🙏 Acknowledgments

- Inspired by modern database migration tools and best practices
- Built with [Cobra](https://github.com/spf13/cobra) and [GORM](https://gorm.io/)
- Thanks to all contributors and the Go community

---

**GoMigrate** - Making database migrations simple, powerful, and reliable.
