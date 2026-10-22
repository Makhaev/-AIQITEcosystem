FROM golang:1.26.1-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/backup ./cmd/backup

FROM postgres:17.10-alpine
COPY --from=build /out/backup /usr/local/bin/aiq-backup
USER 70:70
ENTRYPOINT ["aiq-backup"]
CMD ["serve"]
