# GoGen Migration Hub - Web UI & Collaboration Tools

A comprehensive web-based UI and collaboration platform for database migration management, addressing the gaps typically found in CLI-only tools.

## Features

### 1. Web UI Dashboard ✅
- **Real-time migration status** - Live updates via WebSocket
- **Interactive dashboard** - Visual representation of migration state
- **Migration history** - Browse and analyze past migrations
- **Health monitoring** - System status and metrics

### 2. Migration Catalog ✅
- **Browse migrations** - Visual catalog of all migrations
- **Advanced search** - Full-text search with filters
- **Categorization** - Organize by schema, data, security, etc.
- **Tagging system** - Flexible metadata tagging
- **Version tracking** - Complete version history

### 3. Visual Migration Designer ✅
- **Drag-and-drop interface** - Design migrations visually
- **Schema visualization** - See table relationships
- **Reverse engineering** - Import from existing databases
- **Template library** - Pre-built migration templates
- **Code generation** - Generate SQL, XML, YAML, JSON

### 4. Team Collaboration ✅
- **Review workflows** - Request and provide reviews
- **Approval system** - Multi-approver support
- **Comments** - Line-by-line code comments
- **Team management** - Role-based access control
- **Notifications** - Real-time alerts via WebSocket

## Architecture

```
pkg/core/database/migrations/webui/
├── config/              # Configuration management
│   ├── config.go        # Configuration structures
│   ├── errors.go        # Configuration errors
│   └── config_test.go   # Configuration tests
├── constants/           # Constants and enums
│   └── constants.go     # API routes, error codes, permissions
├── server/              # HTTP server
│   ├── server.go        # Gin-based server
│   ├── templates/       # HTML templates
│   ├── static/          # Static assets (CSS, JS)
│   └── server_test.go   # Server tests
├── middleware/          # HTTP middleware
│   ├── middleware.go    # Auth, RBAC, CORS, rate limiting
│   └── middleware_test.go
├── handlers/            # HTTP handlers
│   ├── migrations/      # Migration API handlers
│   │   ├── handler.go   # Status, execute, rollback, validate
│   │   └── types.go     # Request/response types
│   ├── catalog/         # Catalog API handlers
│   │   ├── handler.go   # Search, browse, stats
│   │   └── types.go     # Catalog types
│   ├── collaboration/   # Collaboration handlers
│   │   ├── handler.go   # Reviews, approvals, comments
│   │   └── types.go     # Collaboration types
│   └── designer/        # Designer handlers
│       ├── handler.go   # Visual design, generation
│       └── types.go     # Designer types
├── websocket/           # WebSocket support
│   ├── hub.go           # WebSocket hub
│   ├── client.go        # WebSocket client
│   └── handler.go       # WebSocket handlers
├── integration.go       # Integration helpers
└── README.md           # This file
```

## Installation

The web UI is integrated into the GoGen framework. No additional installation is required.

## Usage

### Starting the Web UI Server

```bash
# Start with defaults (port 8080)
gogen webui

# Start on custom port
gogen webui --port 9090

# Start on custom host and port
gogen webui --host localhost --port 8000

# Start with configuration file
gogen webui --config webui-config.yaml
```

### Accessing the Dashboard

Once started, access the dashboard at:
- **Dashboard**: http://localhost:8080/static/dashboard.html
- **Health Check**: http://localhost:8080/health
- **API Docs**: http://localhost:8080/api/v1/

## API Endpoints

### Migration Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/migrations/status` | Get migration status |
| POST | `/api/v1/migrations/execute` | Execute migrations |
| POST | `/api/v1/migrations/rollback` | Rollback migrations |
| POST | `/api/v1/migrations/validate` | Validate migrations |
| GET | `/api/v1/migrations/history` | Get migration history |
| POST | `/api/v1/migrations/dry-run` | Dry run migrations |
| POST | `/api/v1/migrations/diff` | Schema diff |

### Catalog Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/migrations/catalog/search` | Search catalog |
| GET | `/api/v1/migrations/catalog` | Browse catalog |
| GET | `/api/v1/migrations/catalog/:id` | Get catalog entry |
| GET | `/api/v1/migrations/catalog/tags` | Get all tags |
| GET | `/api/v1/migrations/catalog/categories` | Get categories |
| GET | `/api/v1/migrations/catalog/stats` | Get statistics |

### Collaboration Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/reviews/create` | Create review |
| GET | `/api/v1/reviews/list` | List reviews |
| GET | `/api/v1/reviews/:id` | Get review |
| POST | `/api/v1/reviews/:id/approve` | Approve review |
| POST | `/api/v1/reviews/:id/reject` | Reject review |
| POST | `/api/v1/reviews/:id/comment` | Add comment |

### Designer Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/migrations/designer/designs` | Create design |
| GET | `/api/v1/migrations/designer/designs` | List designs |
| GET | `/api/v1/migrations/designer/designs/:id` | Get design |
| PUT | `/api/v1/migrations/designer/designs/:id` | Update design |
| POST | `/api/v1/migrations/designer/designs/:id/generate` | Generate migration |
| POST | `/api/v1/migrations/designer/validate` | Validate design |
| POST | `/api/v1/migrations/designer/reverse-engineer` | Reverse engineer |
| GET | `/api/v1/migrations/designer/templates` | Get templates |

### WebSocket Endpoints

| Endpoint | Description |
|----------|-------------|
| `/ws/connect` | WebSocket connection |
| `/ws/stats` | WebSocket statistics |

## WebSocket Channels

Subscribe to real-time updates:

```javascript
const ws = new WebSocket('ws://localhost:8080/ws/connect');

ws.onopen = () => {
    // Subscribe to migration status updates
    ws.send(JSON.stringify({
        action: 'subscribe',
        channel: 'migration:status'
    }));
};

ws.onmessage = (event) => {
    const data = JSON.parse(event.data);
    console.log('Received:', data);
};
```

Available channels:
- `migration:status` - Migration status updates
- `migration:logs` - Migration log entries
- `review:updates` - Review updates
- `team:notifications` - Team notifications

## Configuration

Example `webui-config.yaml`:

```yaml
host: 0.0.0.0
port: 8080
mode: release
readTimeout: 30s
writeTimeout: 30s
shutdownTimeout: 10s
maxRequestSize: 10485760

webSocket:
  enabled: true
  pingInterval: 30s
  pongWait: 60s
  writeWait: 10s
  maxMessageSize: 524288

collaboration:
  enabled: true
  requireReview: false
  minApprovals: 1
  reviewTimeout: 24h
  enableComments: true
  enableNotifications: true

catalog:
  enabled: true
  pageSize: 50
  maxSearchResults: 100
  enableFullText: true
  enableTags: true

designer:
  enabled: true
  maxTablesPerDiagram: 50
  enableAutoLayout: true
  enableValidation: true
  supportedFormats:
    - sql
    - xml
    - yaml
    - json

security:
  enableAuth: true
  enableRBAC: true
  enableAudit: true
  sessionTimeout: 8h
  enableCSRF: true
  enableRateLimit: true
  rateLimitRequests: 100
  rateLimitWindow: 1m

features:
  enableMigrationCatalog: true
  enableDesigner: true
  enableCollaboration: true
  enableRealTimeUpdates: true
  enableDryRun: true
  enableRollback: true
```

## Development

### Running Tests

```bash
# Run all web UI tests
go test ./pkg/core/database/migrations/webui/...

# Run tests with coverage
go test -cover ./pkg/core/database/migrations/webui/...

# Run specific component tests
go test ./pkg/core/database/migrations/webui/server
go test ./pkg/core/database/migrations/webui/middleware
go test ./pkg/core/database/migrations/webui/config
```

### Building

The web UI is built as part of the main GoGen binary:

```bash
go build -o gogen cmd/gogen/main.go
```

### Integration with Existing Server

To integrate the web UI into an existing Gin application:

```go
import (
    "github.com/gogen-framework/gogen/pkg/core/database/migrations/webui"
    "github.com/gogen-framework/gogen/pkg/core/database/migrations/webui/config"
)

// Create web UI configuration
cfg := config.DefaultConfig()

// Register with existing router
hub, err := webui.RegisterWithExistingRouter(router, migrationTool, cfg, logger)
if err != nil {
    log.Fatal(err)
}

// Use hub for broadcasting
hub.BroadcastMigrationStatus(statusData)
```

## Security

The web UI includes comprehensive security features:

- **Authentication** - JWT-based authentication
- **Authorization** - Role-based access control (RBAC)
- **CSRF Protection** - Cross-site request forgery protection
- **Rate Limiting** - Prevent abuse
- **Security Headers** - X-Frame-Options, CSP, etc.
- **Audit Logging** - Track all actions

## Performance

- **WebSocket** - Efficient real-time updates
- **Caching** - Redis-backed caching
- **Compression** - Gzip compression
- **Connection Pooling** - Efficient resource usage

## Comparison with Competitors

| Feature | GoGen | Flyway Desktop | Liquibase Hub |
|---------|-------|---------------|---------------|
| Web UI | ✅ Yes | ✅ Yes | ✅ Yes |
| Migration Designer | ✅ Yes | ✅ Yes | ❌ Limited |
| Team Collaboration | ✅ Yes | ✅ Yes | ✅ Yes |
| Migration Catalog | ✅ Yes | ✅ Yes | ✅ Yes |
| Real-time Updates | ✅ WebSocket | ❌ Polling | ❌ Polling |
| Self-hosted | ✅ Yes | ❌ Desktop only | ❌ Cloud only |
| Open Source | ✅ Yes | ❌ No | ❌ No |

## Contributing

Contributions are welcome! Please see the main project README for contribution guidelines.

## License

See main project LICENSE file.

## Support

For issues and questions:
- GitHub Issues: https://github.com/gogen-framework/gogen/issues
- Documentation: https://docs.gogen.dev
