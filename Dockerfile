# syntax=docker/dockerfile:1.7
#
# NeoCloud container image: the neocloud binary, the web client and the landing
# page. Everything that changes at runtime -- database, encrypted files, .env
# with the master key -- lives in the /data volume.
#
# The web client (flutter_app/build/web) travels with the checkout: every release
# builds and commits it, so the image never needs the Flutter SDK.

FROM --platform=$BUILDPLATFORM golang:1.27-trixie AS build
ARG TARGETOS
ARG TARGETARCH
ENV CGO_ENABLED=0 GOWORK=off
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags "-s -w" -o /out/neocloud ./cmd/neocloud

FROM debian:trixie-slim
LABEL org.opencontainers.image.title="NeoCloud" \
      org.opencontainers.image.description="Self-hosted, encrypted multi-user file storage" \
      org.opencontainers.image.url="https://github.com/NeoLabs-Systems/NeoCloud" \
      org.opencontainers.image.source="https://github.com/NeoLabs-Systems/NeoCloud" \
      org.opencontainers.image.licenses="AGPL-3.0-only"

# Which release channel the image reports.
ARG NEOCLOUD_CHANNEL=stable
# librsvg2-bin and a font render SVG thumbnails, poppler-utils improves PDF text
# search and bubblewrap keeps LibreOffice off the network.
# Office previews need LibreOffice (~400 MB). Build with
# --build-arg WITH_LIBREOFFICE=true to include it.
ARG WITH_LIBREOFFICE=false
# uid 1000 owns the data volumes of earlier images; keep it.
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates tini \
      librsvg2-bin fonts-dejavu-core poppler-utils bubblewrap \
 && if [ "$WITH_LIBREOFFICE" = "true" ]; then apt-get install -y --no-install-recommends libreoffice-core libreoffice-writer libreoffice-calc libreoffice-impress; fi \
 && rm -rf /var/lib/apt/lists/* \
 && groupadd --gid 1000 neocloud \
 && useradd --uid 1000 --gid 1000 --create-home --shell /usr/sbin/nologin neocloud \
 && mkdir /data \
 && chown 1000:1000 /data \
 && chmod 0700 /data

WORKDIR /app
COPY --from=build /out/neocloud /usr/local/bin/neocloud
COPY LICENSE ./
COPY landing ./landing
COPY flutter_app/build/web ./flutter_app/build/web
COPY --chmod=0755 scripts/docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
# Compose files written for the Node.js image check health with `node -e`.
COPY --chmod=0755 scripts/node-healthcheck.sh /usr/local/bin/node

ENV NEOCLOUD_HOME=/data \
    NEOCLOUD_APP_DIR=/app \
    NEOCLOUD_CONTAINER=1 \
    NEOCLOUD_RELEASE_CHANNEL=${NEOCLOUD_CHANNEL} \
    PORT=3344

USER 1000:1000
VOLUME ["/data"]
EXPOSE 3344
# /api/health answers 503 when the database or the data volume is not usable.
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
  CMD ["neocloud", "healthcheck"]
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/docker-entrypoint.sh"]
CMD ["serve"]
