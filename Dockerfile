FROM golang:1.24-alpine AS builder

RUN apk add --no-cache gcc musl-dev sqlite-dev ffmpeg

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 go build -ldflags="-s -w" -o /crate ./cmd/crate

FROM alpine:3.21

RUN apk add --no-cache ffmpeg sqlite-libs ca-certificates tzdata

RUN adduser -D -u 1000 crate

COPY --from=builder /crate /usr/local/bin/crate

RUN mkdir -p /data /music && chown -R crate:crate /data /music

USER crate

EXPOSE 4533

VOLUME ["/data", "/music"]

HEALTHCHECK --interval=30s --timeout=5s --retries=3 \
    CMD wget -q --spider http://localhost:4533/rest/ping.view || exit 1

ENTRYPOINT ["crate"]
CMD ["-config", "/data/crate.json", "-admin-user", "admin", "-admin-password", "admin"]
