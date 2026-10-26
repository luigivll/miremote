#!/usr/bin/env python3
"""Parchea el andamiaje Android generado por `flutter create` en CI:

- Añade los permisos de INTERNET y CÁMARA al AndroidManifest.xml (el QR usa la
  cámara; la señalización/WebRTC usan internet, que en release no se declara
  solo).
- Sube minSdk a 23, que es lo que exige flutter_webrtc.

Es idempotente y tolera tanto build.gradle (Groovy) como build.gradle.kts.
"""
import re
import sys
from pathlib import Path

ANDROID = Path("android")
MANIFEST = ANDROID / "app/src/main/AndroidManifest.xml"

PERMS = """    <uses-permission android:name="android.permission.INTERNET"/>
    <uses-permission android:name="android.permission.ACCESS_NETWORK_STATE"/>
    <uses-permission android:name="android.permission.CHANGE_NETWORK_STATE"/>
    <uses-permission android:name="android.permission.ACCESS_WIFI_STATE"/>
    <uses-permission android:name="android.permission.CAMERA"/>
    <uses-feature android:name="android.hardware.camera" android:required="false"/>
"""


def patch_manifest() -> None:
    text = MANIFEST.read_text(encoding="utf-8")
    if "android.permission.CAMERA" in text:
        print("manifest: permisos ya presentes")
        return
    # Insertar justo después de la etiqueta <manifest ...>.
    m = re.search(r"<manifest\b[^>]*>", text)
    if not m:
        sys.exit("no se encontró la etiqueta <manifest>")
    idx = m.end()
    text = text[:idx] + "\n" + PERMS + text[idx:]
    MANIFEST.write_text(text, encoding="utf-8")
    print("manifest: permisos inyectados")


def patch_gradle() -> None:
    candidates = [
        ANDROID / "app/build.gradle",
        ANDROID / "app/build.gradle.kts",
    ]
    for g in candidates:
        if not g.exists():
            continue
        text = g.read_text(encoding="utf-8")
        orig = text
        # minSdk = flutter.minSdkVersion  |  minSdkVersion flutter.minSdkVersion
        # minSdk = 21  |  minSdkVersion 21
        text = re.sub(
            r"(minSdk(?:Version)?\s*=?\s*)(flutter\.minSdkVersion|\d+)",
            r"\g<1>23",
            text,
        )
        if text != orig:
            g.write_text(text, encoding="utf-8")
            print(f"gradle: minSdk fijado a 23 en {g.name}")
        else:
            print(f"gradle: sin cambios de minSdk en {g.name}")


if __name__ == "__main__":
    if not MANIFEST.exists():
        sys.exit("ejecuta este script dentro de remote_mobile/ tras `flutter create`")
    patch_manifest()
    patch_gradle()
