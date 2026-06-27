FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/surge-web .

FROM alpine:3
RUN apk add --no-cache ca-certificates && \
    adduser -D -u 1000 surge
COPY --from=builder /out/surge-web /usr/local/bin/
ENV XDG_CONFIG_HOME=/var/lib \
    XDG_STATE_HOME=/var/lib
VOLUME ["/var/lib/surge"]
LABEL org.opencontainers.image.source="https://github.com/y1jiong/surge-web"
LABEL org.opencontainers.image.description="Web-based dashboard for the Surge download manager"
LABEL org.opencontainers.image.licenses="Apache-2.0"
USER surge
EXPOSE 1799
ENTRYPOINT ["surge-web"]
