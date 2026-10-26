#!/usr/bin/env bash
# build.sh — compila los componentes Go de MiRemote.
# El cliente Flutter se compila aparte con `flutter build` (ver remote_mobile).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUT="$ROOT/bin"
mkdir -p "$OUT"

echo "==> signaling"
( cd "$ROOT/signaling" && go build -o "$OUT/miremote-signaling" ./cmd/signaling )

echo "==> wol-relay"
( cd "$ROOT/wol-relay" && go build -o "$OUT/miremote-wolrelay" ./cmd/wolrelay )

echo "==> agent (plataforma actual, sin captura real si no es Windows+cgo)"
( cd "$ROOT/agent" && go build -o "$OUT/miremote-agent" ./cmd/agent )

echo
echo "Binarios en $OUT:"
ls -la "$OUT"

cat <<'NOTE'

Para el agente Windows con captura real:
  cd agent
  CGO_ENABLED=1 GOOS=windows go build -ldflags "-H windowsgui" -o MiRemoteAgent.exe ./cmd/agent
  (requiere la DLL winshot; ver agent/native/winshot/README.md)

Para el cliente móvil:
  cd remote_mobile && flutter create . && flutter pub get && flutter build apk
NOTE
