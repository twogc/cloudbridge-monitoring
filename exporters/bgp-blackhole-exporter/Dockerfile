# BGP Blackhole Exporter Dockerfile
FROM golang:1.21-alpine AS builder

WORKDIR /app
RUN apk add --no-cache git ca-certificates
COPY . .
RUN if [ ! -f go.mod ]; then go mod init github.com/twogc/cloudbridge-bgp-blackhole-exporter && go mod tidy; fi
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -ldflags="-w -s" -o bgp-blackhole-exporter .

FROM alpine:latest
WORKDIR /app
RUN apk --no-cache add ca-certificates && \
    addgroup -g 1001 -S appuser && adduser -u 1001 -S appuser -G appuser
COPY --from=builder /app/bgp-blackhole-exporter .
RUN chown -R appuser:appuser /app
USER appuser
EXPOSE 9101
HEALTHCHECK --interval=30s --timeout=5s CMD wget --no-verbose --tries=1 --spider http://localhost:9101/metrics || exit 1
CMD ["./bgp-blackhole-exporter"]
