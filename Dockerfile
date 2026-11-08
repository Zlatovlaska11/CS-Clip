FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o cs-clip .

FROM alpine:3.20
RUN apk add --no-cache ffmpeg
WORKDIR /app
COPY --from=builder /app/cs-clip .
VOLUME ["/app/demos", "/app/clips"]
EXPOSE 8080
CMD ["./cs-clip"]
