FROM golang:1.25-alpine AS builder
RUN apk add --no-cache build-base
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -ldflags="-s -w" -o /waifu-mirror ./cmd/waifu-mirror

FROM alpine:3.21
RUN apk add --no-cache ca-certificates sqlite-libs
COPY --from=builder /waifu-mirror /usr/local/bin/waifu-mirror
RUN mkdir -p /data/images
VOLUME /data
EXPOSE 8420
ENTRYPOINT ["waifu-mirror", "-data", "/data", "-tailnet-only=false"]
