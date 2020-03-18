FROM golang:windowsservercore

COPY . /src

WORKDIR /src

RUN go build

ENTRYPOINT [ "go", "test"]
