FROM golang

COPY . /src

WORKDIR /src

RUN go mod download

ENTRYPOINT [ "go", "test", "-timeout", "1m"]
