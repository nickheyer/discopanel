FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/modules/status-panel/main.go ./
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o status-panel .

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=builder /app/status-panel /status-panel
EXPOSE 8081
CMD ["/status-panel"]
