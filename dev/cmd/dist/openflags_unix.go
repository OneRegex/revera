//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package main

import (
	"os"
	"syscall"
)

// Non-blocking, so a FIFO swapped in after the checks cannot hang the open.
const readOnlyFlags = os.O_RDONLY | syscall.O_NONBLOCK
