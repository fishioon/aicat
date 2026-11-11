//go:build !darwin

package systemproxy

func ownerAlive(int) bool { return false }
