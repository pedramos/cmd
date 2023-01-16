package main

import (
	"crypto/sha512"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

type FileSum struct {
	sum, path string
}

type OutFmt int

const (
	arrayFmt OutFmt = iota
	csvFmt
	tabFmt
	zeroFmt
)

func usage() {
	fmt.Println("dedup [-d PATH] [-p PATTERN] [-o FORMAT] [-k DIR_PATTERN]")
	os.Exit(1)
}

func HashCtl(c chan FileSum, h map[string][]string) {
	for s := range c {
		h[s.sum] = append(h[s.sum], s.path)
		wg.Done()
	}
}

func CalcSum(r io.Reader, path string, c chan FileSum) {
	b, err := io.ReadAll(r)
	if err != nil {
		log.Printf("Couldn't read %s: %v\n", path, err)
		return
	}

	s := FileSum{
		sum:  fmt.Sprintf("%x", sha512.Sum512(b)),
		path: path,
	}
	c <- s
}

var DirFlag = flag.String("d", "./", "Directory containing the files to deduplicate")
var PatternFlag = flag.String("p", ".*", "Regex to find file names")
var OutFormatFlag = flag.String("o", "array", "stdout fmt: csv, array, tab, zero")
var KeepFilesFlag = flag.String("k", "*", "Preserve (keep) files which are in supplied directory")
var ShowAllFlag = flag.Bool("a", false, "Shows all files, without it the command will hide one of the files")

var wg sync.WaitGroup

func main() {
	flag.Parse()
	var outfmt OutFmt
	switch *OutFormatFlag {
	case "csv":
		outfmt = csvFmt
	case "array":
		outfmt = arrayFmt
	case "tab":
		outfmt = tabFmt
	case "zero":
		outfmt = zeroFmt
	default:
		log.Fatal("Invalid output format")
	}

	dupfs := os.DirFS(*DirFlag)
	patternRgx, err := regexp.Compile(*PatternFlag)
	if err != nil {
		log.Fatalf("Incorrect pattern provided: %v", err)
	}

	ch := make(chan FileSum, 100)
	var fileHash = make(map[string][]string)
	go HashCtl(ch, fileHash)

	fillHash := func(path string, d fs.DirEntry, err error) error {
		if d.IsDir() {
			return nil
		}
		if patternRgx.MatchString(d.Name()) {
			f, err := dupfs.Open(path)
			if err != nil {
				log.Printf("Could not open file %v\n", err)
				return nil
			}
			wg.Add(1)
			go CalcSum(f, path, ch)
		}
		return nil
	}
	err = fs.WalkDir(dupfs, ".", fillHash)

	wg.Wait()
	close(ch)
	for sum := range fileHash {
		if len(fileHash[sum]) > 1 {
			var removedOne bool = false
			for i := range fileHash[sum] {
				if i > len(fileHash[sum]) {
					break
				}
				if ismatch, err := filepath.Match(*KeepFilesFlag, fileHash[sum][i]); err == nil && ismatch {
					fileHash[sum], err = remove(fileHash[sum], i)
					removedOne = true
					if err != nil {
						log.Fatalf("Probable bug: %v\n", err)
					}
					break
				} else if err != nil {
					log.Fatalf("Incorrect pattern to -k given\n")
				}
			}
			if removedOne == false && !*ShowAllFlag {
				fileHash[sum], err = remove(fileHash[sum], 0)
			}
			switch outfmt {
			case arrayFmt:
				fmt.Printf("%+q\n", fileHash[sum])
			case csvFmt:
				fmt.Println(strings.Join(fileHash[sum], ";"))
			case tabFmt:
				fmt.Println(strings.Join(fileHash[sum], "	"))
			case zeroFmt:
				fmt.Print("%s\x00", strings.Join(fileHash[sum], "\x00"))
			}
		}
	}
}

func remove(s []string, i int) ([]string, error) {
	if len(s) == 0 || i > len(s) {
		err := fmt.Errorf("Could not remove element %d-th from array with size %d", i, len(s))
		return s, err
	}
	s[i] = s[len(s)-1]
	return s[:len(s)-1], nil
}
