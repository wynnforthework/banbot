FROM golang:1.23

WORKDIR /ban

ENV BanDataDir=/ban/data \
    BanStratDir=/ban/strats

RUN git clone https://github.com/banbox/banstrats /ban/strats

WORKDIR /ban/strats
RUN go mod tidy && \
    go build -o ../bot

WORKDIR /ban
RUN chmod +x bot && \
    ./bot init

ENTRYPOINT ["/ban/bot"] 
