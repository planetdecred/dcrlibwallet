FROM golang 

RUN mkdir /app                        
ADD . /app/                        
WORKDIR /app 

RUN curl -sfL https://install.goreleaser.com/github.com/golangci/golangci-lint.sh | sh -s -- -b $(go env GOPATH)/bin v1.19.1

ENV GO111MODULE on

RUN go mod download

ENTRYPOINT [ "./run_tests.sh", "all"]
