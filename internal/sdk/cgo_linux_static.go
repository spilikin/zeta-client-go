//go:build linux && static

package sdk

// System libs the K/N linuxX64 static archive expects from the host loader.
// Confirmed once a real Linux build runs (CI / native Linux host). Linker
// errors of the form `undefined symbol: foo` after this point belong here.

/*
#cgo linux CFLAGS: -I${SRCDIR} -I${SRCDIR}/prebuilt/linux_amd64
#cgo linux LDFLAGS: -L${SRCDIR}/prebuilt/linux_amd64 -lzeta_sdk -lpthread -ldl -lm -lrt -lresolv -lz

#include "zeta_sdk_glue.h"
*/
import "C"
