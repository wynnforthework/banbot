FROM banbase

ENV BanDataDir=/ban/data \
    BanStratDir=/ban/strats

WORKDIR /ban/strats

RUN git reset --hard HEAD && git pull origin main && \
    go get -u github.com/banbox/banbot && \
    go mod tidy && \
    go build -o ../bot


WORKDIR /ban
RUN chmod +x bot && \
    ./bot init

EXPOSE 8000 8001

ENTRYPOINT ["/ban/bot"]

