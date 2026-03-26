FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o fido .

FROM alpine:3.19
RUN apk add --no-cache ca-certificates git
COPY --from=builder /app/fido /usr/local/bin/fido
ENTRYPOINT ["fido"]
