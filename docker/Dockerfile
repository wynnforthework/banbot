FROM golang:1.23

WORKDIR /ban

RUN git clone https://github.com/banbox/banstrats /ban/strats

WORKDIR /ban/strats
RUN go mod tidy && \
    go build -o ../bot && \
    chmod +x ../bot

WORKDIR /ban

ENTRYPOINT ["/ban/bot"] 
