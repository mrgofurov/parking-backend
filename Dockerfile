FROM golang:1.26-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o server ./cmd/server
# Windows device-agent: the admin panel's "Windows o'rnatuvchi" download is
# this executable with the agent's settings appended (AGENT_WINDOWS_EXE).
ARG AGENT_VERSION=docker
RUN CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="-w -s -X main.version=${AGENT_VERSION}" \
    -o dist/agent/parking-agent.exe ./cmd/device-agent

FROM alpine:latest
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /root/

COPY --from=builder /app/server .
COPY --from=builder /app/dist/agent/parking-agent.exe ./dist/agent/parking-agent.exe
COPY --from=builder /app/.env.example .env

EXPOSE 8080

CMD ["./server"]
