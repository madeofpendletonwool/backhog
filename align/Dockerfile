# syntax=docker/dockerfile:1

# The alignment worker. This image is everything the API image refuses to
# be: it carries ffmpeg, a compiled whisper.cpp and a multi-hundred-
# megabyte model. That is exactly why it lives in its own optional
# container behind a compose profile — a plain `docker compose up` never
# builds or pulls any of it.

# --- whisper.cpp -------------------------------------------------------
# Pinned to a tag, built static so the runtime stage needs no ggml shared
# libraries. Only the CLI is built; the server and the examples are not
# used and would only enlarge the build.
FROM debian:bookworm-slim AS whisper
ARG WHISPER_REF=v1.9.3
RUN apt-get update && apt-get install -y --no-install-recommends \
        build-essential cmake git ca-certificates \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /src
RUN git clone --depth 1 --branch ${WHISPER_REF} https://github.com/ggml-org/whisper.cpp.git .
RUN cmake -B build \
        -DCMAKE_BUILD_TYPE=Release \
        -DBUILD_SHARED_LIBS=OFF \
        -DWHISPER_BUILD_TESTS=OFF \
        -DWHISPER_BUILD_SERVER=OFF \
    && cmake --build build --config Release --target whisper-cli -j "$(nproc)" \
    && install -Dm755 build/bin/whisper-cli /out/whisper-cli

# --- model -------------------------------------------------------------
# Baked into the image at build time so a cold start never waits on
# Hugging Face and an air-gapped host still works. Approximate sizes:
#   tiny.en  ~ 75 MB    base.en  ~ 148 MB (default)
#   small.en ~ 488 MB   medium.en ~ 1.5 GB
# base.en is the CPU-only default: roughly 30-90 minutes for an 11-hour
# book on a decent desktop CPU. small.en is noticeably more accurate and
# roughly three times slower — set WHISPER_MODEL at build time to switch.
FROM debian:bookworm-slim AS model
ARG WHISPER_MODEL=base.en
RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*
RUN curl -fsSL --retry 3 \
        -o /ggml-${WHISPER_MODEL}.bin \
        "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-${WHISPER_MODEL}.bin" \
    && mkdir -p /models \
    && mv /ggml-${WHISPER_MODEL}.bin /models/model.bin \
    && test -s /models/model.bin

# --- worker binary -----------------------------------------------------
FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
        -o /out/align-worker ./cmd/align-worker

# --- runtime -----------------------------------------------------------
# Not distroless: ffmpeg is a real dependency with real shared libraries,
# and pretending otherwise would mean vendoring a build of it. Slim
# Debian with exactly two runtime packages is the honest version.
FROM debian:bookworm-slim
ARG WHISPER_MODEL=base.en
RUN apt-get update && apt-get install -y --no-install-recommends \
        ffmpeg libgomp1 \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --system --uid 10001 --create-home --home-dir /home/align align

COPY --from=whisper /out/whisper-cli /usr/local/bin/whisper-cli
COPY --from=model /models/model.bin /models/model.bin
COPY --from=build /out/align-worker /app/align-worker

ENV WHISPER_BIN=/usr/local/bin/whisper-cli \
    WHISPER_MODEL_PATH=/models/model.bin \
    WHISPER_MODEL=${WHISPER_MODEL} \
    ALIGN_WORK_DIR=/scratch \
    ALIGN_STATUS_ADDR=:8090

RUN mkdir -p /scratch && chown align:align /scratch

# A GPU variant would replace the whisper stage with a CUDA-enabled build
# (-DGGML_CUDA=ON on an nvidia/cuda devel base) and this stage with a
# CUDA runtime base; nothing outside this file would change. Deliberately
# not built here — CPU-only is a one-time per-book cost and needs no host
# driver.

EXPOSE 8090
USER align
WORKDIR /app
ENTRYPOINT ["/app/align-worker"]
