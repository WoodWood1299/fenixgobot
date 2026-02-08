#build stage
FROM golang:alpine AS builder
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .
RUN go build -o /app/fenixgobot -v

#final stage
FROM alpine:3.19
COPY --from=builder /app/fenixgobot /app
RUN addgroup -S botgroup && adduser -D -S botuser -G botgroup
RUN mkdir /data && chown botuser:botgroup /data

USER botuser
ENTRYPOINT ["/app"]
