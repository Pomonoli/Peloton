# syntax=docker/dockerfile:1
FROM golang:1.27.4-alpine AS build
RUN apk add --no-cache git ca-certificates
WORKDIR /src
ADD https://github.com/opencairn-org/cairn-provider-strava.git#aee3c7e6da8f8a702d1b10cf16fdc2b81f9bd729 /src
COPY deploy/strava/cycling-only.patch /tmp/cycling-only.patch
RUN git apply /tmp/cycling-only.patch && go mod download
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -buildid= -X main.workerRelease=v0.7.2" \
    -o /out/worker-strava ./cmd/worker-strava

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/worker-strava /usr/local/bin/worker-strava
COPY --from=build /src/LICENSE /usr/share/licenses/cairn-provider-strava/LICENSE
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/worker-strava"]
