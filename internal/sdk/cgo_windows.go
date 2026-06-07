//go:build windows && !static

package sdk

/*
#cgo windows CFLAGS: -I${SRCDIR} -I${SRCDIR}/../../../zeta-sdk/zeta-sdk/build/bin/mingwX64/debugShared
#cgo windows LDFLAGS: -L${SRCDIR}/../../../zeta-sdk/zeta-sdk/build/bin/mingwX64/debugShared -lzeta_sdk

#include "zeta_sdk_glue.h"
*/
import "C"
