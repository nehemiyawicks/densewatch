# Multi-stage build for either densewatch binary. The repo is one zero-dependency
# Go module, so no module download / network is needed at build time.
#   docker build --build-arg TARGET=simulator     ...
#   docker build --build-arg TARGET=exporters/cdu ...
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY simulator ./simulator
COPY exporters ./exporters
ARG TARGET=simulator
RUN CGO_ENABLED=0 go build -trimpath -o /out/app ./${TARGET}

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/app /app
ENTRYPOINT ["/app"]
