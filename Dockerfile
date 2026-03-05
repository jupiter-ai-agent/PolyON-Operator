FROM golang:1.23-alpine AS builder
WORKDIR /build
COPY go.mod go.sum* ./
COPY *.go ./
COPY wizard.html ./
COPY manifests/ manifests/
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /polyon-operator .

FROM alpine:3.21
ARG TARGETARCH
RUN apk add --no-cache curl \
 && curl -fsSL "https://dl.k8s.io/release/$(curl -fsSL https://dl.k8s.io/release/stable.txt)/bin/linux/${TARGETARCH}/kubectl" \
      -o /usr/local/bin/kubectl \
 && chmod +x /usr/local/bin/kubectl \
 && apk del curl
COPY --from=builder /polyon-operator /usr/local/bin/polyon-operator
EXPOSE 8080
CMD ["polyon-operator"]
