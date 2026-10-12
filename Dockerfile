# syntax=docker/dockerfile:1
# ssh-app — ein statisches Go-Programm in einem minimalen Image (ohne Shell).
# Uebersetzt per Cross-Compile fuer die Zielplattform, also auch arm64 ohne QEMU.
#   Auf dem Zielgeraet selbst (Plattform automatisch):  docker compose up -d --build
#   Fuer ein anderes Geraet (eine Plattform, landet im lokalen Image-Speicher):
#     docker buildx build --platform linux/arm64 -t ssh-app --load .
#   Mehrere Plattformen auf einmal brauchen einen buildx-Builder mit Treiber
#   docker-container (der Standard-Builder kann das nur mit containerd-Image-Speicher)
#   und eine Registry als Ziel:
#     docker buildx create --name ssh-app-bau --driver docker-container --use
#     docker buildx build --platform linux/amd64,linux/arm64 -t <registry>/ssh-app:<version> --push .

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
# Quelle ist ein fester Commit des tldr-Repositorys. Die Release-Archive von tldr-pages
# werden laufend unter gleichem Namen neu hochgeladen, eine Pruefsumme darauf hielte nur
# wenige Tage. Geprueft wird der Inhalt (alle Seiten und LICENSE.md), nicht das tar.gz.
# Neuer Stand: TLDR_COMMIT setzen, bauen, die gemeldete Ist-Pruefsumme eintragen.
FROM --platform=$BUILDPLATFORM alpine:3.22 AS hilfe
ARG TLDR_COMMIT=d5f8a8f335f6b534a980958218e83100d74c772c
ARG TLDR_INHALT_SHA256=f7a3b0d5dd74f1fec2adff23229fda74075140d136d94f15e9da1aaedd9f1b21
WORKDIR /quelle
RUN apk add --no-cache curl zip \
 && curl -fsSL https://github.com/tldr-pages/tldr/archive/${TLDR_COMMIT}.tar.gz \
    | tar -xz --strip-components=1 tldr-${TLDR_COMMIT}/LICENSE.md tldr-${TLDR_COMMIT}/pages tldr-${TLDR_COMMIT}/pages.de \
 && ist=$(find LICENSE.md pages pages.de -type f -exec sha256sum {} + | sort | sha256sum | cut -d' ' -f1) \
 && if [ "$ist" != "${TLDR_INHALT_SHA256}" ]; then echo "tldr-pages: Pruefsumme $ist statt ${TLDR_INHALT_SHA256}" >&2; exit 1; fi \
 && mkdir /hilfe \
 && cp LICENSE.md pages/ && cp LICENSE.md pages.de/ \
 && (cd pages && zip -qrX /hilfe/tldr-pages.en.zip .) \
 && (cd pages.de && zip -qrX /hilfe/tldr-pages.de.zip .)

# --- 3. Laufzeit: distroless, laeuft als Benutzer «nonroot» (65532) --------
FROM gcr.io/distroless/static-debian12:nonroot
LABEL org.opencontainers.image.title="ssh-app" \
      org.opencontainers.image.description="Web SSH client with vault, files and command help" \
      org.opencontainers.image.source="https://github.com/uiff/ssh-app" \
      org.opencontainers.image.vendor="IoTUeli" \
      org.opencontainers.image.licenses="MIT"
COPY --from=bau /out/ssh-app /app/ssh-app
COPY --from=hilfe /hilfe /app/hilfe
# Lizenzhinweise (die Texte der Drittsoftware sind zusaetzlich ins Programm eingebettet: /vendor/licenses/)
COPY LICENSE NOTICE /app/
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
