FROM golang:1.27-trixie AS build

ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/temvia-api ./cmd/server

FROM alpine:3.22
RUN apk add --no-cache ca-certificates
RUN addgroup -S temvia && adduser -S -G temvia temvia
COPY --from=build /out/temvia-api /usr/local/bin/temvia-api
USER temvia
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/temvia-api"]
