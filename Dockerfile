# GoMigrate Dockerfile
# @file Dockerfile
# @description Multi-stage Docker build for the standalone migration tool
# @author Admilson B. F. Cossa
# @date 2025-01-28
# @path pkg/core/database/migrations/Dockerfile

# Build stage
FROM golang:1.21-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git make gcc musl-dev

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN make build

# Runtime stage
FROM alpine:latest

# Install runtime dependencies
RUN apk --no-cache add \
    ca-certificates \
    tzdata \
    curl \
    bash

# Create non-root user
RUN addgroup -g 1001 -S gomigrate && \
    adduser -u 1001 -S gomigrate -G gomigrate

# Set working directory
WORKDIR /app

# Copy binary from builder stage
COPY --from=builder /app/build/gomigrate /usr/local/bin/gomigrate

# Copy example configurations
COPY --from=builder /app/examples/ /app/examples/

# Create directories for migrations and configs
RUN mkdir -p /app/migrations /app/config && \
    chown -R gomigrate:gomigrate /app

# Switch to non-root user
USER gomigrate

# Set environment variables
ENV GOMIGRATE_CONFIG_PATH=/app/config
ENV GOMIGRATE_MIGRATIONS_PATH=/app/migrations

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD gomigrate status || exit 1

# Default command
ENTRYPOINT ["gomigrate"]
CMD ["--help"]

# Labels
LABEL maintainer="Admilson B. F. Cossa <admilson@example.com>"
LABEL version="1.0.0"
LABEL description="GoMigrate - Advanced Database Migration Tool"
LABEL org.opencontainers.image.title="GoMigrate"
LABEL org.opencontainers.image.description="Advanced Database Migration Tool for Go"
LABEL org.opencontainers.image.version="1.0.0"
LABEL org.opencontainers.image.vendor="Your Organization"
LABEL org.opencontainers.image.licenses="MIT"
LABEL org.opencontainers.image.source="https://github.com/your-org/gomigrate"
LABEL org.opencontainers.image.documentation="https://gomigrate.dev"