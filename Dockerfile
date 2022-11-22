FROM golang:1.19-buster as builder

ENV GO111MODULE "on"

ARG BUILD_VER

WORKDIR /usr/local/go/src/app
COPY . .
RUN go mod download
ENV GOOS=linux
ENV GOARCH=amd64
ENV CGO_ENABLED=0

RUN go build \
  -v \
  -ldflags "-w -s -X 'main.BuildDatetime=$(date --iso-8601=seconds)' -X 'main.BuildVer=${BUILD_VER}'" \
  -o server \
  ./main.go

FROM alpine:3.16
WORKDIR /app
COPY --from=builder /usr/local/go/src/app/server /app/
RUN apk add curl jq --no-cache
EXPOSE 8000
ENTRYPOINT ["/app/server"]
