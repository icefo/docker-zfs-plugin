FROM golang:1.23-alpine AS builder

WORKDIR /plugin

ADD go.mod go.sum ./
RUN go mod download

COPY . .

RUN go install

CMD ["/go/bin/docker-zfs-plugin"]

FROM alpine:3
RUN apk upgrade --no-cache && apk add zfs --no-cache
RUN mkdir -p /run/docker/plugins /mnt/state
COPY --from=builder /go/bin/docker-zfs-plugin .
CMD ["docker-zfs-plugin"]
