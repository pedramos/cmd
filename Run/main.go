package main

import (
	"log"
	"os"
	"os/exec"
	"strings"

	"plramos.win/9fans/acme"
)

func usage() {
	log.Println("Run cmd")
	os.Exit(0)
}

func main() {
	wins, err := acme.Windows()
	if err != nil {
		log.Fatal("Not running in acme")
	}

	if len(os.Args) != 2 {
		usage()
	}

	winid := -1
	found := 0
	for _, w := range wins {
		if strings.Contains(w.Name, os.Args[1]) {
			winid = w.ID
			found++
		}
	}
	if found == 0 {
		log.Fatal("Window name does not match")
	} else if found > 1 {
		log.Fatal("Ambigous name")
	}

	w, _ := acme.Open(winid, nil)
	src := w.Selection()
	if src == "" {
		buff, err := w.ReadAll("body")
		if err != nil {
			log.Fatal("failed to read from acme window")
		}
		src = string(buff)
	}
	cmd := exec.Command("rc", "-c", src)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	cmd.Run()
}
