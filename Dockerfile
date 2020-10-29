FROM golang:1.12-alpine as builder
RUN apk update && apk add --update git
WORKDIR /src
COPY ./go.mod ./go.sum ./
RUN go mod download
COPY ./ ./
WORKDIR /src
RUN go build -o scefsimulator ./cmd/sim/*.go

FROM golang:1.12-alpine
RUN apk update
WORKDIR /app
COPY --from=builder /src/scefsimulator .
ENTRYPOINT [ "/app/scefsimulator" ]
