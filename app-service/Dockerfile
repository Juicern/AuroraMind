# syntax=docker/dockerfile:1
FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o auroramind-app main.go

FROM alpine:3.19
WORKDIR /app
ENV APP_PORT=8080
ENV LOCAL_STORAGE_PATH=/data/storage
COPY --from=builder /app/auroramind-app /usr/local/bin/auroramind-app
RUN mkdir -p /data/storage
EXPOSE 8080
CMD ["auroramind-app"]
