#ifndef SDKTEST_GLUE_H
#define SDKTEST_GLUE_H

#include <stdint.h>

#ifdef _WIN32
#include "zeta_sdk_api.h"
#else
#include "libzeta_sdk_api.h"
#endif

void test_call_put   (ZetaSdk_StorageVTable* vt, const char* key, const char* value, uintptr_t cbCtx);
void test_call_get   (ZetaSdk_StorageVTable* vt, const char* key,                    uintptr_t cbCtx);
void test_call_remove(ZetaSdk_StorageVTable* vt, const char* key,                    uintptr_t cbCtx);
void test_call_clear (ZetaSdk_StorageVTable* vt,                                     uintptr_t cbCtx);

void test_call_log(ZetaSdk_LogVTable* vt, const char* level, const char* tag, const char* message);

void test_call_read_certificate     (ZetaSdk_SmcbVTable* vt,                         uintptr_t cbCtx);
void test_call_external_authenticate(ZetaSdk_SmcbVTable* vt, const char* challenge, uintptr_t cbCtx);

#endif
