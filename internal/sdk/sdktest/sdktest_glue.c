#include "_cgo_export.h"
#include "sdktest_glue.h"

void test_call_put(ZetaSdk_StorageVTable* vt, const char* key, const char* value, uintptr_t cbCtx) {
    vt->put(vt->context, key, value, (ZetaSdk_VoidCallback)go_test_void_complete, (void*)cbCtx);
}

void test_call_get(ZetaSdk_StorageVTable* vt, const char* key, uintptr_t cbCtx) {
    vt->get(vt->context, key, (ZetaSdk_StringCallback)go_test_string_complete, (void*)cbCtx);
}

void test_call_remove(ZetaSdk_StorageVTable* vt, const char* key, uintptr_t cbCtx) {
    vt->remove(vt->context, key, (ZetaSdk_VoidCallback)go_test_void_complete, (void*)cbCtx);
}

void test_call_clear(ZetaSdk_StorageVTable* vt, uintptr_t cbCtx) {
    vt->clear(vt->context, (ZetaSdk_VoidCallback)go_test_void_complete, (void*)cbCtx);
}

void test_call_log(ZetaSdk_LogVTable* vt, const char* level, const char* tag, const char* message) {
    vt->log(vt->context, level, tag, message);
}

void test_call_read_certificate(ZetaSdk_SmcbVTable* vt, uintptr_t cbCtx) {
    vt->readCertificate(vt->context, (ZetaSdk_BytesCallback)go_test_bytes_complete, (void*)cbCtx);
}

void test_call_external_authenticate(ZetaSdk_SmcbVTable* vt, const char* challenge, uintptr_t cbCtx) {
    vt->externalAuthenticate(vt->context, challenge, (ZetaSdk_BytesCallback)go_test_bytes_complete, (void*)cbCtx);
}
