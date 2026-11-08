# Stage 1: Build static binary
FROM golang:1.25-alpine AS builder

WORKDIR /src

# Download dependencies to leverage layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy application source
COPY . .

# Compile static binary with symbols stripped
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /bin/cinema-api \
    ./cmd/api

# Stage 2: Minimal non-root runtime image
FROM gcr.io/distroless/static:nonroot

WORKDIR /app

COPY --from=builder /bin/cinema-api /bin/cinema-api

USER 65532:65532

HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
    CMD ["/bin/cinema-api", "healthcheck"]

EXPOSE 8080

ENTRYPOINT ["/bin/cinema-api"]
CMD ["serve"]
