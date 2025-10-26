# Scrubbing Exporter Dockerfile
FROM golang:1.21-alpine AS builder

WORKDIR /app
RUN apk add --no-cache git ca-certificates
COPY . .
RUN if [ ! -f go.mod ]; then go mod init github.com/twogc/cloudbridge-scrubbing-exporter && go mod tidy; fi
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -ldflags="-w -s" -o scrubbing-exporter .

FROM alpine:latest
WORKDIR /app
RUN apk --no-cache add ca-certificates && \
    addgroup -g 1001 -S appuser && adduser -u 1001 -S appuser -G appuser
COPY --from=builder /app/scrubbing-exporter .
RUN chown -R appuser:appuser /app
USER appuser
EXPOSE 9102
HEALTHCHECK --interval=30s --timeout=5s CMD wget --no-verbose --tries=1 --spider http://localhost:9102/metrics || exit 1
CMD ["./scrubbing-exporter"]
