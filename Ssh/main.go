// Ssh cmd is meant to be used inside acme. It manages ssh connections to other systems.
// Each connection must be described in a configuration file with the following template:
//
//	Info about server
//	blah blah
//	--end--
//
//	host <ip|fqdn>
//	user <user>
//	password
//	key <path to keyfile>
//
// Only one of key or password is needed. Password is simply a flag to indicate that the password should be asked.
//
// By default the files will be located in $home/lib/coms/ssh
package main // plramos.win/acme-cmd/Ssh

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"plramos.win/9fans/acme"
)

func usage() {
	fmt.Fprintf(os.Stderr, "usage: Ssh -d directory \n")
	os.Exit(2)
}

var HomeEnv = os.Getenv("HOME")
var MntEnv = os.Getenv("9MNT")
var defaultDir = fmt.Sprintf("%s/lib/coms/ssh", HomeEnv)
var sshDir = flag.String("d", defaultDir, "Directory contianing all the ssh connection description")

type Server struct {
	host, user, key string
	password        bool
}

func main() {
	if MntEnv == "" {
		MntEnv = HomeEnv + "/n"
	}
	w, _ := acme.New()
	w.Name("%s/ssh/+list", MntEnv)
	w.Fprintf("tag", "Get Dial Info Add Mnt")
	fileSystem := os.DirFS(*sshDir)
	writeSshEntries(w, fileSystem)

	for e := range w.EventChan() {
		switch e.C2 {
		case 'x': // execute in tag
			// Get Dial Info Add Rm
			if string(e.Text) == "Get" {
				continue
			}
			if string(e.Text) == "Dial" {
				dial(w, e, fileSystem)
			}
			if string(e.Text) == "Mnt" {
				sshFS(w, e, fileSystem)
			}
			if string(e.Text) == "Del" {
				w.Del(true)
				os.Exit(0)
			}
			if string(e.Text) == "Info" {
				sshConfig := strings.TrimSpace(w.Selection())
				f, err := fileSystem.Open(sshConfig)
				if err != nil {
					w.Fprintf("body", "Cannot open %s: %v\n", sshConfig, err)
					w.Ctl("clean")
				}
				displayInfo(f, sshConfig)
			}
			if string(e.Text) == "Add" {
				createEntry()
			}
		case 'X': // executes in body
			dial(w, e, fileSystem)
		
		case 'L': // right click on body
			sshFS(w, e, fileSystem)
		}
	}
}

func createEntry() {
	w, _ := acme.New()
	winName := fmt.Sprintf("%s/", *sshDir)
	w.Name(winName)
	w.Write("body", []byte(`
--end--
host
username
key
password
`))
	w.Ctl("clean")
}

func writeSshEntries(w *acme.Win, fileSystem fs.FS) {

	fs.WalkDir(fileSystem, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			w.Fprintf("body", "Error on %s: %v\n", path, err)
			w.Ctl("clean")
		}
		if d.IsDir() {
			return nil
		}
		if path != "." {
			w.Fprintf("body", "%s\n", path)
		} else {
			w.Fprintf("body", "%s\n", d.Name())
		}
		return nil
	})
	w.Ctl("clean")
}

func dial(w *acme.Win, e *acme.Event, fileSystem fs.FS) {
	sshConfig := strings.TrimSpace(string(e.Text))
	w.Del(true)
	f, err := fileSystem.Open(sshConfig)
	if err != nil {
		w.Fprintf("body", "Cannot open %s: %v\n", sshConfig, err)
		w.Ctl("clean")
	}
	server, err := parseConfig(f)
	if err != nil {
		log.Fatal(err)
	}

	var sshCmd []string = []string{"ssh", server.host, "-l", server.user}
	if !server.password {
		sshCmd = append(sshCmd, "-i", server.key)
	}
	cmd := exec.Command("win", sshCmd...)
	cmd.Start()
	time.Sleep(500 * time.Millisecond)
	var sshwinID int
	wins, _ := acme.Windows()
	for _, winfo := range wins {
		if winfo.ID > sshwinID && strings.HasSuffix(winfo.Name, "-ssh") {
			sshwinID = winfo.ID

		}
	}
	w, err = acme.Open(sshwinID, nil)
	if err != nil {
		log.Printf("Could not open window with ssh session due to %s", err)
	} else {
		w.Name("%s/ssh/win/%s+sh", MntEnv, sshConfig)
		defer w.Ctl("clean")
		defer w.Fprintf("body", "--SSH TERMINATED--\n")

	}
	cmd.Wait()
}

func displayInfo(f fs.File, path string) error {
	w, _ := acme.New()
	w.Name(fmt.Sprintf("%s/ssh/%s/+info", MntEnv, path))

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "--end--" {
			break
		}
		w.Write("body", scanner.Bytes())
		w.Write("body", []byte("\n"))
	}
	w.Ctl("clean")
	return nil
}

func sshFS(w *acme.Win, e *acme.Event, fileSystem fs.FS) {
	var sshConfig string
	if w.Selection() == "" {
		sshConfig = strings.TrimSpace(string(e.Text))
	} else { 
		sshConfig = strings.TrimSpace(string(w.Selection()))
	}
	w.Del(true)
	f, err := fileSystem.Open(sshConfig)
	if err != nil {
		w.Fprintf("body", "Cannot open %s: %v\n", sshConfig, err)
		w.Ctl("clean")
	}
	s, err := parseConfig(f)
	if err != nil {
		log.Fatal(err)
	}

	mntPoint := fmt.Sprintf("%s/ssh/fs/%s", MntEnv, sshConfig)
	os.MkdirAll(mntPoint, 0770)
	fsCmdArgs := []string{
		"-C",
		"-o",
		fmt.Sprintf("IdentityFile=%s", s.key),
		fmt.Sprintf("%s@%s:", s.user, s.host),
		mntPoint,
	}
	sshFS := exec.Command("sshfs", fsCmdArgs...)
	stderr, _ := sshFS.StderrPipe()
	go io.Copy(os.Stderr, stderr)
	if err := sshFS.Start(); err != nil {
		log.Printf("Could not mount sshfs: %v", err)
	}
	sshDirWin, _ := acme.New()
	sshDirWin.Name(mntPoint)
	sshDirWin.Ctl("get")
}

func parseConfig(f io.Reader) (Server, error) {
	scanner := bufio.NewScanner(f)
	var b strings.Builder
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "--end--" {
			break
		}
		b.WriteString(scanner.Text())
		b.WriteRune('\n')
	}

	var s Server

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		f := strings.Fields(line)
		switch f[0] {
		case "host":
			s.host = f[1]
		case "user":
			s.user = f[1]
		case "password":
			s.password = true
		case "key":
			s.key = f[1]
		}
	}
	if s.host == "" || (!s.password && s.key == "") || s.user == "" {
		return s, fmt.Errorf("Bad ssh descriptor:\nhost: %s\npassword:%v\nuser:%s", s.host, s.password, s.user)
	}
	return s, nil
}
