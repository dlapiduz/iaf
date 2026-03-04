FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /apiserver ./cmd/apiserver
RUN CGO_ENABLED=0 go build -o /controller ./cmd/controller
RUN CGO_ENABLED=0 go build -o /coachserver ./cmd/coachserver

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=builder /apiserver /usr/local/bin/apiserver
COPY --from=builder /controller /usr/local/bin/controller
COPY --from=builder /coachserver /usr/local/bin/coachserver
