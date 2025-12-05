# Build stage
FROM golang:1.25.5-alpine AS builder

WORKDIR /app

# Install git and ca-certificates (often needed for go modules)
RUN apk add --no-cache git ca-certificates

# Download dependencies first (better layer caching)
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source
COPY . .

# Build statically-linked binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o duties-indexer ./cmd

# Runtime stage
FROM gcr.io/distroless/base-debian12

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/duties-indexer /app/duties-indexer

# The service needs no shell; just the binary
ENTRYPOINT ["/app/duties-indexer"]
