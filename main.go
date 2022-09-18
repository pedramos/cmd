package main

import (
	"crypto/sha512"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
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
)

func usage() {
	fmt.Println("dedup [-d PATH] [-p PATTERN] [-o FORMAT]")
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
var OutFormatFlag = flag.String("o", "array", "stdout fmt: csv, array, tab")

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
			switch outfmt {
			case arrayFmt:
				fmt.Printf("%+q\n", fileHash[sum])
			case csvFmt:
				fmt.Println(strings.Join(fileHash[sum], ";"))
			case tabFmt:
				fmt.Println(strings.Join(fileHash[sum], "	"))
			}
		}
	}
}
