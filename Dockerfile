FROM golang:1.23-alpine AS builder

WORKDIR /plugin

ADD go.mod go.sum ./
RUN go mod download

COPY . .

RUN go install


FROM alpine:3
RUN apk upgrade --no-cache && apk add zfs --no-cache
RUN mkdir -p /run/docker/plugins /mnt/state
COPY --from=builder /go/bin/docker-volume-zfs-plugin .
CMD ["docker-volume-zfs-plugin"]