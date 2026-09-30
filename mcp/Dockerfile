# syntax=docker/dockerfile:1

# The backhog MCP server (MAD-468): the optional bridge that lets a user's
# own AI assistant read their library through the spoiler-safe API. Unlike
# the align and ocr workers it imports nothing from ../api — it speaks only
# the public HTTP surface — so the build context stays inside mcp/.

FROM golang:1.26-alpine AS build
WORKDIR /src

# Download modules first so dependency layers cache independently of source.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
        -o /out/backhog-mcp ./cmd/backhog-mcp

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/backhog-mcp /app/backhog-mcp

# The streamable HTTP transport; stdio users run the binary directly
# instead of through this image. BACKHOG_URL and BACKHOG_TOKEN are supplied
# by compose — the token is the only secret this server ever holds.
ENV BACKHOG_MCP_HTTP=:8081

EXPOSE 8081
USER nonroot:nonroot
ENTRYPOINT ["/app/backhog-mcp", "-http", ":8081"]
