//go:build cgo

package microvm

import "testing"

func TestHostnameSlug(t *testing.T) {
	for name, want := range map[string]string{
		"Mãe-d’Água":    "mae-d-agua",
		"Bumba Meu Boi": "bumba-meu-boi",
		"Tëpërësiki":    "teperesiki",
	} {
		if got := hostnameSlug(name); got != want {
			t.Errorf("hostnameSlug(%q) = %q, want %q", name, got, want)
		}
	}
	for _, name := range vmNames {
		slug := hostnameSlug(name)
		if slug == "" || !hostnamePattern.MatchString(slug+"-4294967295") {
			t.Errorf("invalid hostname for %q: %q", name, slug)
		}
	}
}
