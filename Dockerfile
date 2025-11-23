FROM golang:1.25.4-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-w -s" -o main ./cmd/pr-assignment

FROM alpine:3.22.2
RUN adduser -D -H -h /app appuser
WORKDIR /app
COPY --from=builder --chown=appuser /app/main .
USER appuser
EXPOSE 8080 9000
CMD ["./main"]
