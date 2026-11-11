package systemproxy

import "syscall"

func ownerAlive(pid int) bool { return syscall.Kill(pid, 0) == nil }
