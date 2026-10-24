FROM golang:1.24-alpine AS build
# O h3-go compila a biblioteca H3 (C) junto, então o build precisa de cgo.
RUN apk add --no-cache build-base
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=1 go build -trimpath -o /out/no ./cmd/no

# Mesma libc (musl) do estágio de build, então o binário roda aqui sem nada a mais.
FROM alpine:3.20
COPY --from=build /out/no /usr/local/bin/no
COPY modelo ./modelo
EXPOSE 8080
ENTRYPOINT ["no"]
