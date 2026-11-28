# syntax=docker/dockerfile:1
#
# Builds the whole site in three stages and ships only the Go server binary
# (with the site embedded) on a distroless base: no shell, no package
# manager, runs as a non-root user.
#
#   docker build -t ascii-site --build-arg VERSION=$(git describe --always --dirty) \
#     --build-arg COMMIT=$(git rev-parse --short HEAD) .
#   docker run --rm -p 8080:8080 ascii-site

ARG GCC_VERSION=15
ARG PYTHON_VERSION=3.15
ARG GO_VERSION=1.27

# 1. C++ engine. Linked statically so the binary runs in the Python stage
#    whatever C++ runtime that image has.
FROM gcc:${GCC_VERSION} AS engine
WORKDIR /src
COPY engine/ engine/
RUN mkdir -p bin \
 && g++ -std=c++17 -O2 -Wall -Wextra -Wpedantic -static \
      -o bin/asciigen engine/*.cpp \
 && ./bin/asciigen --self-test

# 2. Site builder: runs the engine, assembles dist/, then lints it.
FROM python:${PYTHON_VERSION}-slim AS site
WORKDIR /src
COPY --from=engine /src/bin/asciigen bin/asciigen
COPY builder/ builder/
COPY content/ content/
COPY web/ web/
COPY lab/ lab/
RUN python3 builder/build.py && python3 builder/lint.py

# 3. Go server with the built site embedded.
FROM golang:${GO_VERSION} AS server
ARG VERSION=dev
ARG COMMIT=none
WORKDIR /src/server
COPY server/ ./
RUN find web -mindepth 1 ! -name .gitkeep -exec rm -rf {} +
COPY --from=site /src/dist/ web/
RUN CGO_ENABLED=0 go build -trimpath -mod=readonly \
      -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
      -o /out/site-server .

# 4. Runtime image.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=server /out/site-server /site-server
USER nonroot:nonroot
ENV PORT=8080
EXPOSE 8080
ENTRYPOINT ["/site-server"]
