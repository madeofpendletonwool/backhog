# syntax=docker/dockerfile:1

# The OCR lettering worker. The align-worker pattern a second time:
# everything expensive — here tesseract and its language data — lives in
# its own optional container behind a compose profile, so a plain
# `docker compose up` never builds or pulls any of it, and the API image
# stays distroless with no OCR code inside it.

# --- worker binary -------------------------------------------------------
# Built from ./ocr alone: unlike the alignment worker this one shares no
# code with the API — the pinned normalizer never crosses this boundary
# because the API folds the corpus itself at search time.
FROM golang:1.26-bookworm AS build
WORKDIR /src/ocr
COPY ocr/go.mod ./
COPY ocr/ ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
        -o /out/ocr-worker ./cmd/ocr-worker

# --- runtime -------------------------------------------------------------
# Slim Debian with exactly one runtime package (tesseract-ocr plus its
# English traineddata). Debian ships the fast variant of the models,
# which is the right trade for balloon lettering: the accuracy ceiling is
# the lettering, not the model.
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
        tesseract-ocr tesseract-ocr-eng \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --system --uid 10001 --create-home --home-dir /home/ocr ocr

COPY --from=build /out/ocr-worker /app/ocr-worker

ENV TESSERACT_BIN=/usr/bin/tesseract \
    OCR_LANGUAGE=eng \
    OCR_WORK_DIR=/scratch \
    OCR_STATUS_ADDR=:8091

RUN mkdir -p /scratch && chown ocr:ocr /scratch

EXPOSE 8091
USER ocr
WORKDIR /app
ENTRYPOINT ["/app/ocr-worker"]
