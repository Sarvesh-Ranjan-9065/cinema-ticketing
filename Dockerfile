# Stage 1: Build static binary with pinned alpine builder
FROM golang:1.25-alpine@sha256:c05ba4b73604069d376c4f41346b05374335b5ca0c46fb6dfede5a59f5196931 AS builder

WORKDIR /src

# Download dependencies first to leverage caching
COPY go.mod go.sum ./
RUN go mod download

# Copy application source
COPY . .

# Compile static binary stripping debug symbols and path info
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /bin/cinema-api \
    ./cmd/api

# Stage 2: Minimal non-root runtime image pinned by digest
FROM gcr.io/distroless/static:nonroot@sha256:963fa6c544fe5ce420f1f54fb88b6fb01479f054c8056d0f74cc2c6000df5240

WORKDIR /app

COPY --from=builder /bin/cinema-api /bin/cinema-api

# Run as non-root user (distroless static nonroot UID: 65532)
USER 65532:65532

# Native healthcheck using the binary subcommand without shell or curl
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
    CMD ["/bin/cinema-api", "healthcheck"]

EXPOSE 8080

ENTRYPOINT ["/bin/cinema-api"]
CMD ["serve"]
