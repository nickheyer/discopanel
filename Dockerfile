ARG APP_VERSION=dev

FROM node:22-alpine AS frontend-builder

ARG APP_VERSION
ENV APP_VERSION=${APP_VERSION}

WORKDIR /app/web/discopanel

COPY web/discopanel/package*.json ./
RUN npm ci

COPY web/discopanel/ ./

RUN npm run build

FROM golang:1.24.5-alpine AS backend-builder

ARG APP_VERSION
ENV APP_VERSION=${APP_VERSION}

RUN apk add --no-cache gcc musl-dev sqlite-dev

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=frontend-builder /app/web/discopanel/build ./web/discopanel/build

RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o discopanel ./cmd/discopanel

FROM alpine:latest

ARG APP_VERSION
ENV APP_VERSION=${APP_VERSION}

RUN apk --no-cache add ca-certificates sqlite-libs

WORKDIR /app

COPY --from=backend-builder /app/discopanel .
COPY config.example.yaml ./

#RUN mkdir -p data/servers backups tmp

EXPOSE 8080

CMD ["./discopanel"]