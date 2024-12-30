FROM golang:1.23

WORKDIR /ban

RUN git clone https://github.com/banbox/banstrats /ban/strats

WORKDIR /ban/strats
RUN go mod tidy
