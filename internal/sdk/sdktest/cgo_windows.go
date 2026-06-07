//go:build windows

package sdktest

/*
#cgo windows CFLAGS: -I${SRCDIR} -I${SRCDIR}/../../../../zeta-sdk/zeta-sdk/build/bin/mingwX64/debugShared

#include "sdktest_glue.h"
*/
import "C"
