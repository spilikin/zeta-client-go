//go:build linux && !static

package sdk

/*
#cgo linux CFLAGS: -I${SRCDIR} -I${SRCDIR}/../../../zeta-sdk/zeta-sdk/build/bin/linuxX64/debugShared
#cgo linux LDFLAGS: -L${SRCDIR}/../../../zeta-sdk/zeta-sdk/build/bin/linuxX64/debugShared -lzeta_sdk -Wl,-rpath,$ORIGIN

#include "zeta_sdk_glue.h"
*/
import "C"
