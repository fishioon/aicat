//go:build darwin && cgo && !e2e

package systemproxy

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Foundation -framework SystemConfiguration
#include <stdlib.h>
char *aicat_proxy_read(char **error);
*/
import "C"

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
	"unsafe"
)

const supported = true

func readServices() ([]service, error) {
	var failure *C.char
	value := C.aicat_proxy_read(&failure)
	if failure != nil {
		defer C.free(unsafe.Pointer(failure))
		return nil, errors.New(C.GoString(failure))
	}
	defer C.free(unsafe.Pointer(value))
	var result []service
	err := json.Unmarshal([]byte(C.GoString(value)), &result)
	sortServices(result)
	return result, err
}

func runNetworksetup(args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "/usr/sbin/networksetup", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("networksetup %s: %s (%w)", args[0], strings.TrimSpace(string(output)), err)
	}
	return nil
}
