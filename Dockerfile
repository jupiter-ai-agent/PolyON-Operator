FROM golang:1.23-alpine AS builder
WORKDIR /build
COPY go.mod main.go wizard.html ./
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /polyon-operator .

FROM alpine:3.21
COPY --from=builder /polyon-operator /usr/local/bin/polyon-operator
EXPOSE 8080
CMD ["polyon-operator"]
