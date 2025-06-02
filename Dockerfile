FROM golang:1.24-alpine AS builder

RUN export GOBIN=$HOME/work/bin
WORKDIR /go/src/app
ADD . .
RUN go get -d -v ./...
RUN CGO_ENABLED=0 go build -ldflags="-w -s" -o main .

FROM alpine:3.22
COPY --from=builder /go/src/app/main /app/
WORKDIR /app
CMD ["./main"]