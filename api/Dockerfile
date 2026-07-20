# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS build
WORKDIR /src

# Download modules first so dependency layers cache independently of source.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
# CGO_ENABLED=0 is what lets this run on distroless/static: the sqlite driver
# (modernc.org/sqlite) is pure Go, so there is nothing to link against.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/backhog ./cmd/backhog

# Staged so the next stage can copy it with the right ownership. Docker applies
# this directory's ownership to a fresh named volume mounted over it, which is
# what lets the nonroot user write to /data.
RUN mkdir -p /data/covers

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/backhog /app/backhog
COPY --from=build --chown=nonroot:nonroot /data /data
ENV DATABASE_PATH=/data/backhog.db \
    COVER_DIR=/data/covers \
    ADDR=:8080

EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/backhog"]
