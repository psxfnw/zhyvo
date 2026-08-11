FROM golang:1.26-alpine AS build

WORKDIR /src
RUN apk add --no-cache ca-certificates git

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/worker ./cmd/worker && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate

FROM alpine:3.22 AS runtime-base
RUN apk add --no-cache ca-certificates tzdata && addgroup -S app && adduser -S app -G app
WORKDIR /app
COPY --from=build /out/ /app/bin/
USER app
EXPOSE 8080

FROM runtime-base AS api
CMD ["/app/bin/api"]

FROM runtime-base AS migrate
CMD ["/app/bin/migrate"]

FROM runtime-base AS worker
USER root
RUN apk add --no-cache ffmpeg libheif-tools
USER app
CMD ["/app/bin/worker"]
