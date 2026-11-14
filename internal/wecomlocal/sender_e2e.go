//go:build e2e

package wecomlocal

import "os"

func sendScript() string { return os.Getenv("AICAT_E2E_WECOM_SEND_SCRIPT") }
