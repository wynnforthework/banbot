FROM golang:1.23

WORKDIR /ban

RUN git clone https://github.com/banbox/banstrats /ban/strats

WORKDIR /ban/strats

RUN go get -u github.com/banbox/banbot && \
    go mod download && \
    go build -o ../bot && \
    rm -f ../bot

