# gomigrate container image: a static, CGO-free binary on distroless.
#
#   docker build -t gomigrate .
#   docker run --rm -v "$PWD/migrations:/migrations" -e GOMIGRATE_DSN=postgres://... gomigrate up
#
# @author Admilson B. F. Cossa

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/gomigrate ./cmd/gomigrate

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/gomigrate /usr/local/bin/gomigrate
WORKDIR /work
ENTRYPOINT ["/usr/local/bin/gomigrate", "--dir", "/migrations"]
CMD ["--help"]

LABEL org.opencontainers.image.title="gomigrate" \
      org.opencontainers.image.description="Versioned SQL and Go migrations for PostgreSQL, MySQL and SQLite" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.source="https://github.com/go-gen-ecosystem/go-migrate"
