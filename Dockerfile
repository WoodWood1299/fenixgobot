#build stage
FROM golang:alpine AS builder
#RUN apk add --no-cache git
WORKDIR /go/src/app
COPY go.mod .
COPY go.sum .
COPY main.go .
RUN go mod tidy
COPY . .
RUN go build -o /go/bin/app -v

#final stage
FROM alpine:latest
RUN apk --no-cache add ca-certificates
COPY --from=builder /go/bin/app /app
ENTRYPOINT ["/app"]
LABEL Name=fenixgobot Version=0.0.1
EXPOSE 8080
