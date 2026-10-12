FROM golang:1.26.1-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/server ./cmd/server && \
    CGO_ENABLED=0 go build -trimpath -o /out/admin ./cmd/admin

FROM alpine:3.23.3
RUN addgroup -S aiq && adduser -S aiq -G aiq
WORKDIR /app
COPY --from=build /out/server /out/admin ./
USER aiq
EXPOSE 8080
ENTRYPOINT ["/app/server"]
