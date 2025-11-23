FROM golang:1.25-alpine3.22 AS builder

WORKDIR /go/src/github.com/guillembonet/pg-backup
ADD . .
RUN go build -o build/pg-backup .

FROM postgres:18.1-alpine3.22

RUN apk update && apk upgrade

COPY --from=builder /go/src/github.com/guillembonet/pg-backup/build/pg-backup /usr/bin/pg-backup
COPY --from=builder /go/src/github.com/guillembonet/pg-backup/backup.sh /var/scripts/backup.sh
RUN chmod +x /var/scripts/backup.sh

ENTRYPOINT ["/usr/bin/pg-backup"]
