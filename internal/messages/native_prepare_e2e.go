//go:build e2e

package messages

import "os"

func nativePreparationEnvironment() []string {
	if script := os.Getenv("AICAT_E2E_WECOM_INITIALIZER"); script != "" {
		return []string{"AICAT_PREPARE_INITIALIZER=" + script}
	}
	return nil
}

func addNativeTestContext(values map[string]string) {
	if script := os.Getenv("AICAT_E2E_WECOM_INITIALIZER"); script != "" {
		values["initializer"] = script
	}
	if script := os.Getenv("AICAT_E2E_WECOM_SEND_SCRIPT"); script != "" {
		values["sendScript"] = script
	}
}
