FROM golang:1.25-alpine AS build

ENV CGO_ENABLED=0

RUN apk add --no-cache git curl

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o ./gitshop ./cmd/server/main.go

FROM golang:1.25-alpine AS dev

ENV CGO_ENABLED=0

WORKDIR /app

RUN apk add --no-cache git nodejs npm make
RUN git config --global --add safe.directory /app

ARG DLV_VERSION=v1.25.2
ARG AIR_VERSION=v1.64.5
ARG TEMPL_VERSION=v0.3.977
ARG TEMPLUI_VERSION=v1.4.0
ARG SQLC_VERSION=v1.30.0
ARG GOLANGCI_LINT_VERSION=v2.8.0

RUN go install github.com/go-delve/delve/cmd/dlv@${DLV_VERSION} && \
	go install github.com/air-verse/air@${AIR_VERSION} && \
	go install github.com/a-h/templ/cmd/templ@${TEMPL_VERSION} && \
	go install github.com/templui/templui/cmd/templui@${TEMPLUI_VERSION} && \
	go install github.com/sqlc-dev/sqlc/cmd/sqlc@${SQLC_VERSION} && \
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_LINT_VERSION}

COPY . .

CMD air -c .air.toml

FROM alpine:latest AS prod

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=build /app/gitshop ./gitshop

EXPOSE 8080

CMD ["./gitshop"]
