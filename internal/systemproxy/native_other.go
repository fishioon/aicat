//go:build (!darwin || !cgo) && !e2e

package systemproxy

import "errors"

const supported = false

func readServices() ([]service, error) { return nil, errors.New("当前平台不支持系统代理") }
func runNetworksetup([]string) error   { return errors.New("当前平台不支持系统代理") }
