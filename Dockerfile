# syntax=docker/dockerfile:1

FROM golang:1.25-alpine AS build
WORKDIR /src

RUN apk add --no-cache ca-certificates git

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /opt/app ./cmd/

FROM gcr.io/distroless/static:latest
COPY --from=build /opt/app /app
ENTRYPOINT ["/app"]
