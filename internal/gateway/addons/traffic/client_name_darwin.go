//go:build darwin && cgo

package traffic

/*
#cgo LDFLAGS: -framework Foundation
#include <stdlib.h>
char *aicat_client_app(int pid, char **bundleID);
*/
import "C"

import (
	"unsafe"

	store "github.com/fishioon/aicat/internal/traffic"
)

func appForProcess(owner socketOwner) store.ClientApp {
	var bundleID *C.char
	name := C.aicat_client_app(C.int(owner.pid), &bundleID)
	defer C.free(unsafe.Pointer(name))
	defer C.free(unsafe.Pointer(bundleID))
	if name == nil {
		return store.ClientApp{Name: owner.command}
	}
	return store.ClientApp{Name: C.GoString(name), BundleID: C.GoString(bundleID)}
}
