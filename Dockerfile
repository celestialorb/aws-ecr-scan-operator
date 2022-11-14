FROM golang:1.19 as builder

WORKDIR /opt/go
COPY go.mod ./
COPY go.sum ./
COPY *.go ./

RUN go mod tidy
RUN CGO_ENABLED=1 GOEXPERIMENT=boringcrypto go build -o operator main.go

FROM gcr.io/distroless/base-debian11:nonroot

WORKDIR /opt/go
COPY --from=builder /opt/go/operator /opt/go/operator
ENTRYPOINT ["/opt/go/operator"]