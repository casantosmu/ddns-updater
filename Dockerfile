FROM golang:1.24.5 AS build
WORKDIR /app
COPY . ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /ddns-updater

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /ddns-updater /ddns-updater
ENTRYPOINT ["/ddns-updater"]
