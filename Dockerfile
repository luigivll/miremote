# Imagen del servidor de señalización de MiRemote.
# Sirve para Fly.io, Google Cloud Run, Render, Railway, etc.
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY signaling/ ./signaling/
WORKDIR /src/signaling
RUN CGO_ENABLED=0 go build -ldflags "-s -w" -o /miremote-signaling ./cmd/signaling

FROM alpine:3.20
RUN adduser -D -h /home/app app && mkdir -p /data && chown app /data
COPY --from=build /miremote-signaling /usr/local/bin/miremote-signaling
USER app
# Por defecto guarda en /tmp (escribible en cualquier plataforma). En Fly.io se
# monta un volumen en /data y se fija MR_DB ahí (ver fly.toml) para que los
# emparejamientos sobrevivan a reinicios.
ENV MR_DB=/tmp/miremote-signaling.db
ENV PORT=8080
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/miremote-signaling"]
