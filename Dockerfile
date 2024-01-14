FROM golang:1.21 AS builder
WORKDIR /app
COPY . /app/
RUN make linux_amd64 && ls -lR /app/bin

FROM ubuntu:22.04
COPY --from=builder /app/bin/statexec-linux-amd64 /usr/local/bin/statexec
