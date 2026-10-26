// winshot.cpp — implementación de la captura DXGI + codificación H.264 por
// Media Foundation. Compilar con MSVC o MinGW-w64 enlazando contra:
//   d3d11 dxgi mfplat mfuuid mfreadwrite ole32
//
// Notas de diseño:
//   - DXGI Desktop Duplication entrega la imagen del escritorio como una
//     textura D3D11 en GPU, con regiones modificadas e info del cursor. Aquí se
//     toma el frame completo y se pasa al codificador; una optimización
//     posterior es usar las dirty rects y saltar frames idénticos.
//   - El Sink Writer / Transform de Media Foundation para H.264 selecciona un
//     MFT hardware cuando existe (NVENC / Intel Quick Sync / AMD AMF), de modo
//     que la codificación no pasa por CPU. Se pide salida Annex-B.
//   - Para mantener baja latencia: sin B-frames, GOP controlado, y keyframe
//     bajo demanda vía CODECAPI_AVEncVideoForceKeyFrame.
//
// Este archivo es el esqueleto funcional del pipeline. Las rutas de error se
// simplifican por claridad; en producción conviene registrar cada HRESULT.

#include "winshot.h"

#include <windows.h>
#include <d3d11.h>
#include <dxgi1_2.h>
#include <mfapi.h>
#include <mfidl.h>
#include <mfreadwrite.h>
#include <codecapi.h>
#include <wrl/client.h>
#include <vector>
#include <mutex>
#include <cstring>

using Microsoft::WRL::ComPtr;

struct winshot_ctx {
    int width = 0, height = 0, fps = 0, bitrate = 0;

    ComPtr<ID3D11Device> device;
    ComPtr<ID3D11DeviceContext> context;
    ComPtr<IDXGIOutputDuplication> dupl;
    ComPtr<ID3D11Texture2D> stagingTex;

    ComPtr<IMFTransform> encoder; // H.264 MFT (hardware si disponible)
    ComPtr<IMFSample>    outSample;

    std::vector<uint8_t> frameBuf; // buffer entregado a Go
    std::mutex mu;
    bool forceKeyframe = false;
    LONGLONG frameIndex = 0;
    LONGLONG duration100ns = 0;
};

// ---- Inicialización D3D11 + DXGI duplication ----

static bool initCapture(winshot_ctx* c) {
    D3D_FEATURE_LEVEL fl;
    HRESULT hr = D3D11CreateDevice(
        nullptr, D3D_DRIVER_TYPE_HARDWARE, nullptr, 0,
        nullptr, 0, D3D11_SDK_VERSION,
        c->device.GetAddressOf(), &fl, c->context.GetAddressOf());
    if (FAILED(hr)) return false;

    ComPtr<IDXGIDevice> dxgiDev;
    if (FAILED(c->device.As(&dxgiDev))) return false;

    ComPtr<IDXGIAdapter> adapter;
    if (FAILED(dxgiDev->GetAdapter(adapter.GetAddressOf()))) return false;

    ComPtr<IDXGIOutput> output;
    if (FAILED(adapter->EnumOutputs(0, output.GetAddressOf()))) return false;

    ComPtr<IDXGIOutput1> output1;
    if (FAILED(output.As(&output1))) return false;

    hr = output1->DuplicateOutput(c->device.Get(), c->dupl.GetAddressOf());
    if (FAILED(hr)) return false;

    return true;
}

// ---- Inicialización del codificador H.264 por Media Foundation ----

static bool initEncoder(winshot_ctx* c) {
    // Tipo de salida H.264.
    ComPtr<IMFMediaType> outType;
    MFCreateMediaType(outType.GetAddressOf());
    outType->SetGUID(MF_MT_MAJOR_TYPE, MFMediaType_Video);
    outType->SetGUID(MF_MT_SUBTYPE, MFVideoFormat_H264);
    outType->SetUINT32(MF_MT_AVG_BITRATE, c->bitrate);
    MFSetAttributeSize(outType.Get(), MF_MT_FRAME_SIZE, c->width, c->height);
    MFSetAttributeRatio(outType.Get(), MF_MT_FRAME_RATE, c->fps, 1);
    MFSetAttributeRatio(outType.Get(), MF_MT_PIXEL_ASPECT_RATIO, 1, 1);
    outType->SetUINT32(MF_MT_INTERLACE_MODE, MFVideoInterlace_Progressive);
    outType->SetUINT32(MF_MT_MPEG2_PROFILE, eAVEncH264VProfile_Base);

    // Tipo de entrada NV12 (lo que entrega el convertidor desde BGRA).
    ComPtr<IMFMediaType> inType;
    MFCreateMediaType(inType.GetAddressOf());
    inType->SetGUID(MF_MT_MAJOR_TYPE, MFMediaType_Video);
    inType->SetGUID(MF_MT_SUBTYPE, MFVideoFormat_NV12);
    MFSetAttributeSize(inType.Get(), MF_MT_FRAME_SIZE, c->width, c->height);
    MFSetAttributeRatio(inType.Get(), MF_MT_FRAME_RATE, c->fps, 1);
    inType->SetUINT32(MF_MT_INTERLACE_MODE, MFVideoInterlace_Progressive);

    // Aquí se crearía el MFT H.264 (CLSID_CMSH264EncoderMFT) y se intentaría
    // activar un MFT hardware con MFTEnumEx(MFT_ENUM_FLAG_HARDWARE). Por
    // brevedad del esqueleto, se omite el enlazado del Sink Writer/Transform
    // completo; el pipeline real:
    //   1. BGRA (DXGI) -> NV12 (Video Processor MFT o shader).
    //   2. NV12 -> H.264 (MFT hardware).
    //   3. Sample de salida -> Annex-B -> frameBuf.
    (void)inType;
    return true;
}

extern "C" winshot_ctx* winshot_create(int width, int height, int fps, int bitrate) {
    if (width <= 0 || height <= 0 || fps <= 0 || bitrate <= 0) return nullptr;

    MFStartup(MF_VERSION);

    auto* c = new winshot_ctx();
    c->width = width; c->height = height; c->fps = fps; c->bitrate = bitrate;
    c->duration100ns = 10000000LL / fps;

    if (!initCapture(c)) { delete c; return nullptr; }
    if (!initEncoder(c)) { delete c; return nullptr; }

    return c;
}

// winshot_next_frame: captura un frame, lo codifica y entrega la unidad de
// acceso. En este esqueleto, la ruta de codificación hardware está indicada
// pero no cableada de extremo a extremo; la función devuelve timeout cuando no
// hay frame disponible para no bloquear al llamante Go.
extern "C" int winshot_next_frame(winshot_ctx* c, const uint8_t** out_buf,
                                  int* out_len, int* out_keyframe, int timeout_ms) {
    if (!c) return -1;

    ComPtr<IDXGIResource> desktopRes;
    DXGI_OUTDUPL_FRAME_INFO info;
    HRESULT hr = c->dupl->AcquireNextFrame(timeout_ms, &info, desktopRes.GetAddressOf());
    if (hr == DXGI_ERROR_WAIT_TIMEOUT) {
        return 1; // sin cambios en el escritorio: no re-enviar
    }
    if (FAILED(hr)) return -2;

    // --- Aquí va: copiar textura, convertir a NV12, codificar a H.264 ---
    // El resultado (Annex-B) se dejaría en c->frameBuf.

    c->dupl->ReleaseFrame();

    std::lock_guard<std::mutex> lock(c->mu);
    bool key = c->forceKeyframe || (c->frameIndex == 0);
    c->forceKeyframe = false;
    c->frameIndex++;

    // Placeholder de salida mientras no está cableado el encoder completo.
    *out_buf = c->frameBuf.empty() ? nullptr : c->frameBuf.data();
    *out_len = (int)c->frameBuf.size();
    *out_keyframe = key ? 1 : 0;
    return 0;
}

extern "C" int winshot_request_keyframe(winshot_ctx* c) {
    if (!c) return -1;
    std::lock_guard<std::mutex> lock(c->mu);
    c->forceKeyframe = true;
    // En el encoder real: ICodecAPI->SetValue(CODECAPI_AVEncVideoForceKeyFrame, 1).
    return 0;
}

extern "C" int winshot_set_bitrate(winshot_ctx* c, int bitrate) {
    if (!c || bitrate <= 0) return -1;
    std::lock_guard<std::mutex> lock(c->mu);
    c->bitrate = bitrate;
    // En el encoder real: ICodecAPI->SetValue(CODECAPI_AVEncCommonMeanBitRate, ...).
    return 0;
}

extern "C" void winshot_destroy(winshot_ctx* c) {
    if (!c) return;
    MFShutdown();
    delete c;
}
