FROM golang:1.19 as builder

ENV CGO_ENABLED=1
ENV GOEXPERIMENT=boringcrypto

WORKDIR /opt/go
COPY go.mod ./
COPY go.sum ./
COPY *.go ./

RUN go mod tidy
RUN go build -o operator main.go

FROM golang:1.19

WORKDIR /opt/go
COPY --from=builder /opt/go/operator /opt/go/operator