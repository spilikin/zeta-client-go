//go:build darwin

package sdktest

/*
#cgo darwin CFLAGS: -I${SRCDIR} -I${SRCDIR}/../../../../zeta-sdk/zeta-sdk/build/bin/macosArm64/debugShared

#include "sdktest_glue.h"
*/
import "C"
