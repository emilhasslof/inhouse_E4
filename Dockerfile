# Build stage — compiles only the server binary.
# modernc.org/sqlite is pure Go, so CGO_ENABLED=0 is safe.
FROM golang:1.25-alpine AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o server ./cmd/server

# Runtime stage — minimal image with just the binary.
FROM alpine:3.20
WORKDIR /app
COPY --from=build /app/server .

# DB_PATH is overridden at runtime via Railway (or other) env vars.
# Default points to /data which is created automatically by the server on startup.
ENV DB_PATH=/data/inhouse.db
ENV PORT=8080

EXPOSE 8080
CMD ["./server"]
