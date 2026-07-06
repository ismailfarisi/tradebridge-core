# Multi-stage build for the Core API. Build the gateway with --target and
# BINARY=gateway, or add a second stage as needed.
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/api ./cmd/api \
 && CGO_ENABLED=0 go build -o /out/gateway ./cmd/gateway

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/api /api
COPY --from=build /out/gateway /gateway
EXPOSE 8080 8081
ENTRYPOINT ["/api"]
