# Multi-stage build. Pass --build-arg CMD=<name> to pick which binary to ship.
ARG GO_VERSION=1.26
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS build
ARG CMD
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum* ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/${CMD} ./cmd/${CMD}

FROM gcr.io/distroless/static-debian12:nonroot
ARG CMD
COPY --from=build /out/${CMD} /usr/local/bin/cadoo-app
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/cadoo-app"]
