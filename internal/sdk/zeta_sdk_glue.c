#include "_cgo_export.h"
#include "zeta_sdk_glue.h"

void zeta_storage_vtable_init(ZetaSdk_StorageVTable* vt, uintptr_t ctx) {
    vt->context = (void*)ctx;
    vt->put     = (void (*)(void*, const char*, const char*, ZetaSdk_VoidCallback,   void*))go_storage_put;
    vt->get     = (void (*)(void*, const char*,              ZetaSdk_StringCallback, void*))go_storage_get;
    vt->remove  = (void (*)(void*, const char*,              ZetaSdk_VoidCallback,   void*))go_storage_remove;
    vt->clear   = (void (*)(void*,                           ZetaSdk_VoidCallback,   void*))go_storage_clear;
}

void zeta_log_vtable_init(ZetaSdk_LogVTable* vt, uintptr_t ctx) {
    vt->context = (void*)ctx;
    vt->log     = (ZetaSdk_LogCallback)go_log_callback;
    vt->logLevel = ZETA_LOG_LEVEL_DEBUG;
}

void zeta_smcb_vtable_init(ZetaSdk_SmcbVTable* vt, uintptr_t ctx) {
    vt->context              = (void*)ctx;
    vt->readCertificate      = (void (*)(void*, ZetaSdk_BytesCallback, void*))go_smcb_read_certificate;
    vt->externalAuthenticate = (void (*)(void*, const char*, ZetaSdk_BytesCallback, void*))go_smcb_external_authenticate;
}

void zeta_invoke_void_cb(ZetaSdk_VoidCallback cb, void* cbCtx) {
    if (cb != NULL) cb(cbCtx);
}

void zeta_invoke_string_cb(ZetaSdk_StringCallback cb, void* cbCtx, const char* value) {
    if (cb != NULL) cb(cbCtx, value);
}

void zeta_invoke_bytes_cb(ZetaSdk_BytesCallback cb, void* cbCtx, const uint8_t* data, int size) {
    if (cb != NULL) cb(cbCtx, data, size);
}

void zeta_ws_invoke(void* client, const char* url, int urlLen, void* headers, int headerCount) {
    ZetaSdk_Client_ws(client, (void*)url, urlLen, (void*)go_ws_handler, headers, headerCount);
}

int zeta_wsmsg_type(ZetaSdk_WSMessage* msg) {
    return (int)msg->type;
}

const char* zeta_wsmsg_text(ZetaSdk_WSMessage* msg, int* size) {
    *size = msg->data.text.size;
    return msg->data.text.text;
}

const char* zeta_wsmsg_binary(ZetaSdk_WSMessage* msg, int* size) {
    *size = msg->data.binary.size;
    return msg->data.binary.bytes;
}
