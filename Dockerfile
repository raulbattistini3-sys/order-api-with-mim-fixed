# Builds either Go command: --build-arg CMD=api or CMD=gateway.
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY cmd ./cmd
COPY configs ./configs
COPY internal ./internal
ARG CMD=api
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/app ./cmd/${CMD}
RUN mkdir -p /out/imports

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/app /app
COPY config.toml /config.toml
# Owned by the nonroot user, so a new volume mounted here is writable.
COPY --from=build --chown=65532:65532 /out/imports /imports
ENTRYPOINT ["/app"]
