// Command icons converts Hugeicons into the <symbol> sprite embedded in the
// Studio and delivery pages. It reads the ESM modules of an unpacked
// @hugeicons/core-free-icons package:
//
//	curl -sL https://registry.npmjs.org/@hugeicons/core-free-icons/-/core-free-icons-4.3.5.tgz | tar xz
//	go run ./scripts/icons package Sent=send Folder01=folder ...
//
// Each argument is an icon name without the Icon suffix, then = and the
// sprite id, which pages reference as <use href="#i-ID"/>.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	element   = regexp.MustCompile(`\["(\w+)", \{([^}]*)\}\]`)
	attribute = regexp.MustCompile(`(\w+): "([^"]*)"`)
	upper     = regexp.MustCompile(`[A-Z]`)
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: go run ./scripts/icons PACKAGE_DIR Name=id ...")
		os.Exit(2)
	}
	for _, pair := range os.Args[2:] {
		name, id, ok := strings.Cut(pair, "=")
		if !ok {
			fmt.Fprintf(os.Stderr, "icons: %q is not Name=id\n", pair)
			os.Exit(2)
		}
		source, err := os.ReadFile(filepath.Join(os.Args[1], "dist", "esm", name+"Icon.js"))
		if err != nil {
			fmt.Fprintln(os.Stderr, "icons:", err)
			os.Exit(1)
		}
		var body strings.Builder
		for _, match := range element.FindAllStringSubmatch(string(source), -1) {
			body.WriteString("<" + match[1])
			for _, attr := range attribute.FindAllStringSubmatch(match[2], -1) {
				if attr[1] == "key" {
					continue
				}
				key := upper.ReplaceAllStringFunc(attr[1], func(letter string) string { return "-" + strings.ToLower(letter) })
				fmt.Fprintf(&body, ` %s="%s"`, key, attr[2])
			}
			body.WriteString("/>")
		}
		fmt.Printf("<symbol id=\"i-%s\" viewBox=\"0 0 24 24\" fill=\"none\">%s</symbol>\n", id, body.String())
	}
}
