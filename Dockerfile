# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app
COPY agent/ ./agent/
COPY backend/ ./backend/

WORKDIR /app/backend
RUN go build -o /app/bin/aegis-server ./cmd/server

WORKDIR /app/agent
RUN go build -o /app/bin/aegis-agent ./cmd/aegis-agent

# Runtime stage
FROM alpine:3.19

RUN apk --no-cache add ca-certificates curl bash util-linux

WORKDIR /root/
COPY --from=builder /app/bin/aegis-server /usr/local/bin/
COPY --from=builder /app/bin/aegis-agent /usr/local/bin/

EXPOSE 8080

ENTRYPOINT ["aegis-server"]
