FROM golang:1.23

WORKDIR /ban

RUN git clone https://github.com/banbox/banstrats /ban/strats

WORKDIR /ban/strats
RUN go mod tidy && \
    go build -o ../bot

WORKDIR /ban
RUN chmod +x bot && \
    echo "export BanDataDir=/ban/data" >> /etc/profile && \
    echo "export BanStratDir=/ban/strats" >> /etc/profile && \
    source /etc/profile && \
    ./bot init

ENTRYPOINT ["/ban/bot"] 
