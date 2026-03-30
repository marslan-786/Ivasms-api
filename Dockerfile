# پہلا مرحلہ: بلڈ اسٹیج (Build Stage)
FROM golang:1.25-bookworm AS go-builder

# ورکنگ ڈائریکٹری سیٹ کریں
WORKDIR /app

# آپ کی گو (Go) فائلیں کاپی کریں
COPY main.go token.go ./

# لوکل go.mod فائل کے بغیر ماڈیول بنائیں اور پروجیکٹ بلڈ کریں
RUN go mod init ivasms_scraper && \
    go mod tidy && \
    go build -o server .

# دوسرا مرحلہ: رن ٹائم اسٹیج (Run Stage) تاکہ فائنل امیج ہلکی ہو
FROM debian:bookworm-slim

# سرٹیفکیٹس اپڈیٹ کریں تاکہ HTTPS کالز میں کوئی مسئلہ نہ آئے
RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# پچھلے مرحلے سے بلڈ کی ہوئی سرور فائل کاپی کریں
COPY --from=go-builder /app/server .

# پورٹ 8080 اوپن کریں
EXPOSE 8080

# پروگرام کو رن کریں
CMD ["./server"]
