//go:build cgo

package microvm

import (
	_ "embed"
	"math/rand/v2"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

//go:embed brazilian_mythology_names.txt
var mythologyNames string

var vmNames = strings.Split(strings.TrimSpace(mythologyNames), "\n")
var hostnamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

func vmHostname(suffix string) string {
	return hostnameSlug(vmNames[rand.IntN(len(vmNames))]) + "-" + suffix
}

func hostnameSlug(name string) string {
	var out strings.Builder
	separator := false
	for _, r := range norm.NFD.String(strings.ToLower(name)) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			if separator && out.Len() > 0 {
				out.WriteByte('-')
			}
			out.WriteRune(r)
			separator = false
		case unicode.Is(unicode.Mn, r):
			// Discard decomposed accents without splitting the word.
		default:
			separator = true
		}
	}
	return out.String()
}
