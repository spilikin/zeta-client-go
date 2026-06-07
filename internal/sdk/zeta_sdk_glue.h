#ifndef ZETA_SDK_GLUE_H
#define ZETA_SDK_GLUE_H

#include <stdint.h>

// K/N header naming quirk: the mingwX64 shared target strips the `lib` prefix
// (only platform that does); every other case — Unix shared, all static
// targets — keeps it. Define ZETA_STATIC via CFLAGS for static builds.
#if defined(_WIN32) && !defined(ZETA_STATIC)
#include "zeta_sdk_api.h"
#else
#include "libzeta_sdk_api.h"
#endif

void zeta_storage_vtable_init(ZetaSdk_StorageVTable* vt, uintptr_t ctx);
void zeta_log_vtable_init(ZetaSdk_LogVTable* vt, uintptr_t ctx);
void zeta_smcb_vtable_init(ZetaSdk_SmcbVTable* vt, uintptr_t ctx);
void zeta_invoke_void_cb(ZetaSdk_VoidCallback cb, void* cbCtx);
void zeta_invoke_string_cb(ZetaSdk_StringCallback cb, void* cbCtx, const char* value);
void zeta_invoke_bytes_cb(ZetaSdk_BytesCallback cb, void* cbCtx, const uint8_t* data, int size);

void zeta_ws_invoke(void* client, const char* url, int urlLen, void* headers, int headerCount);
int zeta_wsmsg_type(ZetaSdk_WSMessage* msg);
const char* zeta_wsmsg_text(ZetaSdk_WSMessage* msg, int* size);
const char* zeta_wsmsg_binary(ZetaSdk_WSMessage* msg, int* size);

#endif
