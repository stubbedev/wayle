package recorder

import (
	"os"
	"syscall"
)

// signal delivers sig to the running process.
func signal(p *os.Process, sig os.Signal) {
	if p != nil {
		_ = p.Signal(sig)
	}
}

const (
	syscallSIGUSR1 = syscall.SIGUSR1
	syscallSIGUSR2 = syscall.SIGUSR2
	syscallSIGINT  = syscall.SIGINT
)
