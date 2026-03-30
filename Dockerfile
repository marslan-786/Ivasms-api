FROM golang:1.25-bookworm AS go-builder

WORKDIR /app

COPY main.go token.go ./

RUN go mod init ivasms_scraper && \
    go mod tidy && \
    go build -o server .

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=go-builder /app/server .

EXPOSE 8080

CMD ["./server"]
