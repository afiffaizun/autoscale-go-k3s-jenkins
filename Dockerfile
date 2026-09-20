FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY go.mod ./
COPY app ./app

RUN go test ./...

RUN go build -o go-app ./app

FROM alpine:3.22

WORKDIR /app

COPY --from=builder /app/go-app .

EXPOSE 8080

CMD ["./go-app"]
