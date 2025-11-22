package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"9fans.net/go/plan9/client"
	"9fans.net/go/plan9/mux9p"
)

var chattyfuse int

type stdio struct{ r, w *os.File }

func (rw *stdio) Read(p []byte) (int, error)  { return rw.r.Read(p) }
func (rw *stdio) Write(p []byte) (int, error) { return rw.w.Write(p) }

func post9pservice(rfd, wfd *os.File, name, mtpt string) error {
	if name == "" && mtpt == "" {
		rfd.Close()
		wfd.Close()
		return fmt.Errorf("nothing to do")
	}

	if name != "" {
		var network, addr string
		if strings.Contains(addr, "!") { // assume is already network address
			part := strings.SplitN(name, "!", 2)
			network, addr = part[0], part[1]
		} else {
			network = "unix"
			addr = filepath.Join(client.Namespace(), name)
		}
		go func() {
			err := mux9p.Listen(network, addr, &stdio{r: rfd, w: wfd}, &mux9p.Config{})
			if err != nil {
				log.Printf("9p multiplexer failed: %v", err)
			}
		}()
		if mtpt != "" {
 			// reopen
 			log.Fatalf("post9pservice mount not implemented")
		}
	}
	if mtpt != "" {
		log.Fatalf("post9pservice mount not implemented")
	}
	return nil
}
