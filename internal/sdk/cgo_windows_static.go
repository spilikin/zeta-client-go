//go:build windows && static

package sdk

// System libs the K/N mingwX64 static archive expects from the host loader.
// Confirmed once a real Windows build runs (CI / native Windows host). Linker
// errors of the form `undefined symbol: foo` after this point belong here.

/*
#cgo windows CFLAGS: -DZETA_STATIC -I${SRCDIR} -I${SRCDIR}/prebuilt/windows_amd64
// HOME_KONAN is interpolated by the cgo preprocessor — we resolve it via a
// build flag in build-geta-static-windows to find libiconv.a in K/N's
// msys2 cache. Symbols-by-archive cycles (libssl ↔ libcrypto, libcurl ↔
// libngtcp2_crypto_ossl, etc.) are handled by --start-group / --end-group.
#cgo windows LDFLAGS: -L${SRCDIR}/prebuilt/windows_amd64 -L${SRCDIR}/prebuilt/windows_amd64/deps -static -Wl,--start-group -lzeta_sdk -lcurl -lssl -lcrypto -lngtcp2_crypto_ossl -lngtcp2 -lnghttp3 -lnghttp2 -lz -liconv -Wl,--end-group -static-libgcc -static-libstdc++ -lstdc++ -lpthread -lws2_32 -lcrypt32 -ladvapi32 -luserenv -lbcrypt -lncrypt -lsecur32 -lwinhttp -liphlpapi

#include "zeta_sdk_glue.h"
*/
import "C"
