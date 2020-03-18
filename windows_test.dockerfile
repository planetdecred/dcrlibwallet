FROM golang:nanoserver

COPY . /src

WORKDIR /src

RUN go mod download

ENTRYPOINT ["go", "test"]