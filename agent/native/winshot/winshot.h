/*
 * winshot — componente nativo de captura de pantalla y codificación H.264 para
 * Windows, consumido por el agente Go vía cgo.
 *
 * Pipeline:
 *   DXGI Desktop Duplication  -> textura en GPU
 *   -> Media Foundation H.264 (usa el codificador hardware si está disponible:
 *      NVENC/Quick Sync/AMF se exponen como MFT hardware)
 *   -> unidades de acceso Annex-B entregadas a Go.
 *
 * La API es C puro para cruzar la frontera cgo sin complicaciones de C++.
 * Todas las funciones devuelven 0 en éxito y un código negativo en error.
 */
#ifndef WINSHOT_H
#define WINSHOT_H

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

typedef struct winshot_ctx winshot_ctx;

/* Crea el contexto de captura+codificación.
 *  width,height: resolución de salida deseada (se escala desde el escritorio).
 *  fps:          fotogramas por segundo objetivo.
 *  bitrate:      bitrate objetivo inicial en bits/s.
 * Devuelve el contexto o NULL en error. */
winshot_ctx* winshot_create(int width, int height, int fps, int bitrate);

/* Obtiene la siguiente unidad de acceso H.264 (Annex-B).
 *  out_buf:   recibe un puntero a un buffer propiedad del contexto, válido
 *             hasta la siguiente llamada a winshot_next_frame o winshot_destroy.
 *  out_len:   longitud en bytes.
 *  out_keyframe: 1 si es IDR.
 *  timeout_ms: espera máxima por un fotograma nuevo.
 * Retorno: 0 éxito, 1 timeout (sin cambios), <0 error. */
int winshot_next_frame(winshot_ctx* ctx, const uint8_t** out_buf,
                       int* out_len, int* out_keyframe, int timeout_ms);

/* Fuerza un IDR en el siguiente fotograma (respuesta a PLI/FIR). */
int winshot_request_keyframe(winshot_ctx* ctx);

/* Ajusta el bitrate objetivo (respuesta a congestión). */
int winshot_set_bitrate(winshot_ctx* ctx, int bitrate);

/* Libera el contexto. */
void winshot_destroy(winshot_ctx* ctx);

#ifdef __cplusplus
}
#endif

#endif /* WINSHOT_H */
