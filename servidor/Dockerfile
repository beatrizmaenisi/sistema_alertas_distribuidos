FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -o /out/no ./cmd/no

FROM alpine:3.20
COPY --from=build /out/no /usr/local/bin/no
COPY modelo ./modelo
EXPOSE 8080
ENTRYPOINT ["no"]
