FROM golang:1.24.5 AS build-stage

WORKDIR /app
COPY . ./

RUN CGO_ENABLED=0 GOOS=linux go build -o /ddns-updater

FROM gcr.io/distroless/base-debian11

WORKDIR /
COPY --from=build-stage /ddns-updater /ddns-updater

USER nonroot:nonroot

ENTRYPOINT ["/ddns-updater"]
