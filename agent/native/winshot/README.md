# winshot — captura DXGI + codificación H.264 (Windows)

Componente nativo que el agente Go consume por cgo. Captura el escritorio con
DXGI Desktop Duplication y codifica a H.264 con Media Foundation, usando el
codificador hardware cuando está disponible (NVENC / Intel Quick Sync / AMD
AMF se exponen como MFT hardware).

## Compilar (MinGW-w64)

```bash
g++ -shared -O2 -o winshot.dll winshot.cpp \
    -ld3d11 -ldxgi -lmfplat -lmfuuid -lmfreadwrite -lole32 \
    -Wl,--out-implib,libwinshot.a
```

## Compilar (MSVC)

```bat
cl /LD /O2 winshot.cpp d3d11.lib dxgi.lib mfplat.lib mfuuid.lib mfreadwrite.lib ole32.lib
```

Deja `winshot.dll` junto a `MiRemoteAgent.exe` (o en el PATH) y compila el
agente con `CGO_ENABLED=1`.

## Estado

El archivo `winshot.cpp` implementa el esqueleto del pipeline: inicializa D3D11,
la duplicación de escritorio DXGI y declara los tipos de entrada/salida de
Media Foundation. La ruta completa de codificación (BGRA→NV12→H.264 por MFT
hardware y empaquetado Annex-B) está señalada en el código con los puntos
exactos donde se cablea; requiere un equipo Windows con GPU para probarla de
extremo a extremo. El contrato C (`winshot.h`) y el puente cgo
(`capture_windows_cgo.go`) ya están completos y estables, de modo que terminar
el encoder no cambia nada del resto del sistema.

## API (winshot.h)

- `winshot_create(w,h,fps,bitrate)` → contexto
- `winshot_next_frame(...)` → siguiente unidad de acceso H.264 Annex-B
- `winshot_request_keyframe(...)` → fuerza IDR (respuesta a PLI/FIR)
- `winshot_set_bitrate(...)` → ajusta bitrate (respuesta a congestión)
- `winshot_destroy(...)`
