# syntax=docker/dockerfile:1
# ssh-app — ein statisches Go-Programm in einem minimalen Image (ohne Shell).
# Baut fuer die Zielplattform per Cross-Compile, also auch arm64 ohne QEMU:
#   docker buildx build --platform linux/amd64,linux/arm64 -t ssh-app .

ARG GO_VERSION=1.26

# --- 1. Programm uebersetzen ---------------------------------------------
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS bau
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=0.1.0
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY web ./web
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/ssh-app ./cmd/ssh-app \
 && mkdir -p /out/data

# --- 2. Befehlshilfe (tldr-pages, CC BY 4.0) holen und pruefen -------------
FROM --platform=$BUILDPLATFORM alpine:3.22 AS hilfe
ARG TLDR_VERSION=v2.3
ARG TLDR_DE_SHA256=cd0813c39958bfff092ab8269b27146b32361443b2dfe0856602aae31d8c77d6
ARG TLDR_EN_SHA256=286138f1b5fd7cc645acf0bba3d70ca48594c7695efc5f72e34017d50a47b777
WORKDIR /hilfe
RUN apk add --no-cache curl \
 && curl -fsSL -o tldr-pages.de.zip https://github.com/tldr-pages/tldr/releases/download/${TLDR_VERSION}/tldr-pages.de.zip \
 && curl -fsSL -o tldr-pages.en.zip https://github.com/tldr-pages/tldr/releases/download/${TLDR_VERSION}/tldr-pages.en.zip \
 && echo "${TLDR_DE_SHA256}  tldr-pages.de.zip" | sha256sum -c - \
 && echo "${TLDR_EN_SHA256}  tldr-pages.en.zip" | sha256sum -c -

# --- 3. Laufzeit: distroless, laeuft als Benutzer «nonroot» (65532) --------
FROM gcr.io/distroless/static-debian12:nonroot
LABEL org.opencontainers.image.title="ssh-app" \
      org.opencontainers.image.description="Web-SSH-Client mit Tresor, Dateileiste und Befehlshilfe" \
      org.opencontainers.image.source="https://github.com/uiff/ssh-app" \
      org.opencontainers.image.vendor="IoTUeli" \
      org.opencontainers.image.licenses="MIT"
COPY --from=bau /out/ssh-app /app/ssh-app
COPY --from=hilfe /hilfe /app/hilfe
COPY --from=bau --chown=65532:65532 /out/data /data
ENV SSHAPP_DATA=/data \
    SSHAPP_HILFE=/app/hilfe \
    SSHAPP_ADDR=:8443 \
    SSHAPP_TLS=auto
EXPOSE 8443
VOLUME ["/data"]
USER nonroot
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["/app/ssh-app", "gesund"]
ENTRYPOINT ["/app/ssh-app"]
