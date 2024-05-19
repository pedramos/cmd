package main

import (
	"bufio"
	"bytes"
	_ "embed"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unsafe"

	"golang.org/x/exp/maps"
	"gopkg.in/yaml.v3"
	"plramos.win/9fans/acme"
)

//go:embed plr.latex
var templatedata []byte

var KBDir = os.Getenv("kbstore")

type KBArticle struct {
	Tags []string `yaml:"tags"`
	Name string
}

type KBArticles []*KBArticle

func (arts KBArticles) Len() int { return len(arts) }
func (arts KBArticles) Less(i, j int) bool {
	return uintptr(unsafe.Pointer(arts[i])) < uintptr(unsafe.Pointer(arts[j]))
}
func (arts KBArticles) Swap(i, j int) { arts[i], arts[j] = arts[j], arts[i] }

type artIndex map[string][]*KBArticle

func (idx artIndex) String() string {
	var sb strings.Builder
	alltags := maps.Keys(idx)
	sort.Strings(alltags)
	for _, t := range alltags {
		sb.WriteString(t)
		sb.WriteRune('\n')
		for i := range idx[t] {
			sb.WriteRune('\t')
			sb.WriteString(idx[t][i].Name)
			sb.WriteRune('\n')
		}
		sb.WriteRune('\n')
	}
	return sb.String()
}

func (idx artIndex) Tags() []string {
	alltags := maps.Keys(idx)
	sort.Strings(alltags)
	return alltags
}

func (idx artIndex) AllArticles() []*KBArticle {
	var arts KBArticles
	for _, articles := range idx {
		arts = append(arts, articles...)
	}

	sort.Sort(arts)

	unique := make([]*KBArticle, 0, len(arts))
	j := 0
	for i := range arts {
		if len(unique) == 0 {
			unique = append(unique, arts[i])
			continue
		}
		if arts[i] != unique[j] {
			unique = append(unique, arts[i])
			j++
		}
	}
	return unique
}

func (idx artIndex) Filter(tags []string) artIndex {
	for i := range tags {
		if tags[i] == "" {
			tags = append(tags[:i], tags[i+1:]...)
		}
	}
	if len(tags) == 0 {
		return idx
	}

	arts := idx.AllArticles()
	var filtered artIndex = make(map[string][]*KBArticle)

	found := 0
	for i := range arts {
		for _, at := range arts[i].Tags {
			for _, ft := range tags {
				if ft == at {
					found++
				}
			}
		}
		if len(tags) == found {
			filtered = updateTagIdx(arts[i], filtered)
		}
		found = 0
	}
	return filtered
}

//TODO:
// - Better error reporting
// - Print only tags and open on bottom3
// - Expand command to show tags and articles (maybe also flag)
// - Index command when navigating articles
// - Create command to create new article (maybe notes/new is enough?)
// - Look command to look for text in kb

func main() {
	log.SetFlags(log.Llongfile)

	if KBDir == "" {
		KBDir = "./"
	}
	kbfs := os.DirFS(KBDir)

	var tagIdx artIndex = make(map[string][]*KBArticle)

	filesInKb, err := fs.ReadDir(kbfs, ".")
	if err != nil {
		log.Fatal(err)
	}

	for i := range filesInKb {
		a, err := parseMeta(kbfs, filesInKb[i].Name())
		if err != nil {
			log.Fatal(err)
		}
		tagIdx = updateTagIdx(a, tagIdx)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go tagsWinThread(tagIdx, os.Args[1:], &wg)
	wg.Wait()
}

func updateTagIdx(a *KBArticle, idx artIndex) artIndex {
	for i, t := range a.Tags {
		a.Tags[i] = strings.ToLower(t)
		t := strings.ToLower(t)
		idx[t] = append(idx[t], a)
	}
	return idx
}

func parseMeta(kbfs fs.FS, filename string) (*KBArticle, error) {
	f, err := kbfs.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("error in file %s: %w", filename, err)
	}
	s := bufio.NewScanner(f)
	var buff bytes.Buffer
	s.Scan()
	buff.Write(s.Bytes())
	for s.Scan() {
		buff.Write(s.Bytes())
		buff.WriteRune('\n')
		if s.Text() == "---" {
			break
		}
	}
	a := &KBArticle{}
	a.Name = filename
	if err := yaml.Unmarshal(buff.Bytes(), a); err != nil {
		return nil, fmt.Errorf("error in file %s:%w", filename, err)
	}
	return a, nil
}

func tagsWinThread(tagIdx artIndex, filter []string, wg *sync.WaitGroup) {
	defer wg.Done()
	win, err := acme.New()
	if err != nil {
		log.Fatal(err)
	}
	defer win.CloseFiles()
	winname := fmt.Sprintf("/n/notes/tags/%s", path.Join(filter...))
	win.Fprintf("tag", "Get New Pdf Web")
	win.Name(winname)
Redraw:
	win.Clear()
	win.Fprintf("body", "kbstore = %s\n-------\n\n", KBDir)
	win.Fprintf("body", "%s", tagIdx.Filter(filter))
	win.Ctl("clean")
	win.Addr("0,0")
	win.Ctl("dot=addr")
	win.Ctl("show")

EventLoop:
	for e := range win.EventChan() {
		switch e.C2 {
		case 'x', 'X': // execute in tag
			switch string(e.Text) {
			case "Get":
				tag, err := win.ReadAll("tag")
				if err != nil {
					log.Fatal(err)
				}
				winname = strings.Split(string(tag), " Del")[0]
				filter = strings.Split(strings.TrimPrefix(winname, "/n/notes/tags/"), "/")
				goto Redraw
			case "New":
				// TODO New article
			case "Pdf":
				tmpdir := os.TempDir()
				templatefile := path.Join(tmpdir, "plr.latex")
				_, error := os.Stat(templatefile)
				if os.IsNotExist(error) {
					if err := os.WriteFile(templatefile, templatedata, 0666); err != nil {
						log.Fatalf("Could not save template: %v", err)
					}
					defer os.Remove(templatefile)
				}
				_, err := exec.LookPath("pandoc")
				if err != nil {
					acme.Errf(winname, "error looking for pandoc: %v", err)
					continue
				}
				filetorender := strings.TrimSpace(win.Selection())
				tmpf, err := os.CreateTemp("", "anotes-*-"+filetorender+".pdf")
				if err != nil {
					acme.Errf(winname, "Could not create tmp file: %v", err)
					continue
				}
				outpdf := tmpf.Name()
				defer os.Remove(outpdf)
				cmd := exec.Command(
					"pandoc",
					"--pdf-engine", "tectonic", "--toc",
					"--template", templatefile, "--listings",
					path.Join(KBDir, filetorender),
					"-o", outpdf,
				)
				message, _ := cmd.CombinedOutput()
				if len(message) > 0 {
					acme.Errf(winname, "pandoc: %s", string(message))
				}
				cmd = exec.Command("plumb", outpdf)
				message, _ = cmd.CombinedOutput()
				if len(message) > 0 {
					acme.Errf(winname, "plumb: %s", string(message))
				}
			case "Web":
				_, err := exec.LookPath("pandoc")
				if err != nil {
					acme.Errf(winname, "error looking for pandoc: %v", err)
					continue
				}
				filetorender := strings.TrimSpace(win.Selection())
				tmpf, err := os.CreateTemp("", "anotes-*-"+filetorender+".html")
				if err != nil {
					acme.Errf(winname, "Could not create tmp file: %v", err)
					continue
				}
				outfile := tmpf.Name()
				defer os.Remove(outfile)
				cmd := exec.Command(
					"pandoc",
					path.Join(KBDir, filetorender),
					"-o", outfile,
				)
				message, _ := cmd.CombinedOutput()
				if len(message) > 0 {
					acme.Errf(winname, "pandoc: %s", string(message))
				}
				cmd = exec.Command("plumb", "-d", "web", outfile)
				message, _ = cmd.CombinedOutput()
				if len(message) > 0 {
					acme.Errf(winname, "plumb: %s", string(message))
				}
			default:
				win.WriteEvent(e)
			}
		case 'L':
			if _, err := os.Stat(string(e.Text)); !os.IsNotExist(err) {
				win.WriteEvent(e)
				continue				
			}
			// right click on article tag
			var wq0 int
			if e.Q0 == 0 {
				wq0 = 0
			} else {
				wq0 = e.Q0 - 1
			}
			win.Addr("#%d,#%d", wq0, wq0+1)
			b, err := win.ReadAll("xdata")
			if err != nil {
				log.Fatal(err)
			}
			if string(b) == "\n" || wq0 == 0 {
				newfilter := make([]string, len(filter))
				copy(newfilter, filter)
				newfilter = append(newfilter, string(e.Text))
				wg.Add(1)
				go tagsWinThread(tagIdx, newfilter, wg)
				continue
			}
			// right click on article name
			var (
				fpath string
				q0    int = e.Q0
				q1    int = e.Q1
			)
			for {
				win.Addr("#%d,#%d", q0, q1)
				b, _ := win.ReadAll("xdata")
				xdata := []rune(string(b))
				fpath = path.Join(KBDir, strings.TrimSpace(string(b)))
				_, error := os.Stat(fpath)
				if os.IsNotExist(error) {
					if win.Selection() != "" {
						win.WriteEvent(e)
						continue EventLoop
					}
					if !unicode.IsSpace(xdata[0]) {
						q0--
						continue
					}
					r := xdata[len(xdata)-1]
					if !unicode.IsSpace(r) {
						q1++
						continue
					}
					win.WriteEvent(e)
					continue EventLoop
				} else {

					break
				}
			}
			cmd := exec.Command(
				"plumb",
				"-d", "edit",
				fpath,
			)
			message, _ := cmd.CombinedOutput()
			if len(message) > 0 {
				acme.Errf(winname, "plumb: %s", string(message))
			} else {
				win.Del(true)
				return
			}
		default:
			win.WriteEvent(e)
		}
	}
}
func articleWinThread(path string, tag string) {
	win, err := acme.New()
	if err != nil {
		log.Fatal(err)
	}
	defer win.CloseFiles()
	win.Name("%s", KBDir)
	win.Ctl("clean")
	win.Addr("0,0")
	win.Ctl("dot=addr")
	win.Ctl("show")
	win.Fprintf("tag", "New")
}
