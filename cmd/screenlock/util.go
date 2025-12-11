package main

import (
	"os/user"
)

func getuser() string {
	u, err := user.Current()
	if err != nil {
		return "unknown"
	}
	return u.Username
}
