//go:build linux

package sdktest

/*
#cgo linux CFLAGS: -I${SRCDIR} -I${SRCDIR}/../../../../zeta-sdk/zeta-sdk/build/bin/linuxX64/debugShared

#include "sdktest_glue.h"
*/
import "C"
