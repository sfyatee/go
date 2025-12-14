package main

import (
	"os/user"

	"golang.org/x/sys/unix"
)

func getuser() string {
	u, err := user.Current()
	if err != nil {
		return "unknown"
	}
	return u.Username
}

func CreateShm() {
	fd, err := unix.MemfdCreate("buffer", unix.MFD_CLOEXEC)
	if err != nil {
		return
	}
	defer unix.Close(fd)
}
