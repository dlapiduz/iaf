FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /apiserver ./cmd/apiserver
RUN CGO_ENABLED=0 go build -o /controller ./cmd/controller

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=builder /apiserver /usr/local/bin/apiserver
COPY --from=builder /controller /usr/local/bin/controller
