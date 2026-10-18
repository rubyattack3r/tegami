package envelope

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestCatalogsMatch(t *testing.T) {
	placeholders := regexp.MustCompile(`\{\w+\}`)
	english := catalogs["en"]
	for _, language := range languages {
		catalog := catalogs[language]
		if len(catalog) != len(english) {
			t.Errorf("%s has %d keys, English has %d", language, len(catalog), len(english))
		}
		for key, source := range english {
			translation, ok := catalog[key]
			if !ok || strings.TrimSpace(translation) == "" {
				t.Errorf("%s is missing %q", language, key)
				continue
			}
			want, got := placeholders.FindAllString(source, -1), placeholders.FindAllString(translation, -1)
			slices.Sort(want)
			slices.Sort(got)
			if !slices.Equal(want, got) {
				t.Errorf("%s %q uses placeholders %v, English uses %v", language, key, got, want)
			}
		}
	}
}

func TestPagesUseKnownKeys(t *testing.T) {
	for _, name := range []string{"studio.html", "page.html", "studio.js", "page.js"} {
		for _, key := range usedKeys(asset(name)) {
			if _, ok := catalogs["en"][key]; !ok {
				t.Errorf("%s uses unknown key %q", name, key)
			}
		}
	}
}

func TestDeliveryPageCarriesItsText(t *testing.T) {
	data, err := seal("report.txt", []byte("report\n"), []byte("correct horse battery staple"))
	if err != nil {
		t.Fatal(err)
	}
	page, err := renderPage(data)
	if err != nil {
		t.Fatal(err)
	}
	catalog := regexp.MustCompile(`<script id="tegami-i18n" type="application/json">([^<]*)</script>`).FindSubmatch(page)
	if len(catalog) != 2 {
		t.Fatal("delivery page has no catalog")
	}
	var embedded map[string]map[string]string
	if err := json.Unmarshal(catalog[1], &embedded); err != nil {
		t.Fatal(err)
	}
	for _, key := range usedKeys(string(page)) {
		for _, language := range languages {
			if _, ok := embedded[language][key]; !ok {
				t.Errorf("delivery page uses %q, missing from its %s catalog", key, language)
			}
		}
	}
}

// TestAssetsAreNotDoubleEncoded catches UTF-8 text that was encoded twice,
// which turns 手 into "æ‰‹".
func TestAssetsAreNotDoubleEncoded(t *testing.T) {
	doubleEncoded := regexp.MustCompile(`[\x{00C2}\x{00C3}][\x{0080}-\x{00BF}]`)
	entries, err := assets.ReadDir("assets")
	if err != nil {
		t.Fatal(err)
	}
	names := []string{"i18n/en.json", "i18n/ja.json"}
	for _, entry := range entries {
		if !entry.IsDir() && entry.Name() != "argon2.wasm" {
			names = append(names, entry.Name())
		}
	}
	for _, name := range names {
		if match := doubleEncoded.FindString(asset(name)); match != "" {
			t.Errorf("%s contains double-encoded text %q", name, match)
		}
	}
}
