//go:build e2e

package extension

import "os"

func platformEndpoint() string { return os.Getenv("AICAT_E2E_WECOM_URL") }
