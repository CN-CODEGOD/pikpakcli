FROM golang:1.21-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o pikpakidm .

FROM alpine:latest

RUN apk add --no-cache ca-certificates

COPY --from=builder /app/pikpakidm /usr/local/bin/pikpakidm

ENTRYPOINT ["pikpakidm"]
