//go:build darwin && static

package sdk

/*
#cgo darwin CFLAGS: -I${SRCDIR} -I${SRCDIR}/prebuilt/darwin_arm64
#cgo darwin LDFLAGS: -L${SRCDIR}/prebuilt/darwin_arm64 -lzeta_sdk -framework Foundation -framework CoreFoundation -framework Security -framework SystemConfiguration -lz -lc++

#include "zeta_sdk_glue.h"
*/
import "C"
