//go:build darwin && !static

package sdk

/*
#cgo darwin CFLAGS: -I${SRCDIR} -I${SRCDIR}/../../../zeta-sdk/zeta-sdk/build/bin/macosArm64/debugShared
#cgo darwin LDFLAGS: -L${SRCDIR}/../../../zeta-sdk/zeta-sdk/build/bin/macosArm64/debugShared -lzeta_sdk -Wl,-rpath,${SRCDIR}/../../../zeta-sdk/zeta-sdk/build/bin/macosArm64/debugShared

#include "zeta_sdk_glue.h"
*/
import "C"
