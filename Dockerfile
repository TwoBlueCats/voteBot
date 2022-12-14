## Build
FROM golang:1.18-buster AS build

WORKDIR /app

COPY go.mod ./
COPY go.sum ./
RUN go mod download

COPY *.go ./

RUN go build -o /bot-main

## Deploy
FROM gcr.io/distroless/base-debian10

WORKDIR /

COPY --from=build /bot-main /bot-main

USER nonroot:nonroot

ENTRYPOINT ["/bot-main"]