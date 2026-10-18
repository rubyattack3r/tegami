package envelope

import (
	"encoding/json"
	"regexp"
	"strings"
)

// The browser pages and the ZIP README are translated from the catalogs in
// assets/i18n. English is the source and the fallback; every catalog must
// have the same keys and placeholders.
var languages = []string{"en", "ja"}

var catalogs = func() map[string]map[string]string {
	result := map[string]map[string]string{}
	for _, language := range languages {
		var catalog map[string]string
		if err := json.Unmarshal([]byte(asset("i18n/"+language+".json")), &catalog); err != nil {
			panic(err)
		}
		result[language] = catalog
	}
	return result
}()

// i18nMarker is replaced in each page with every catalog, which the page
// reads as data to show the reader's language.
const i18nMarker = `<script id="tegami-i18n" type="application/json">__TEGAMI_I18N__</script>`

// i18nKeys finds the catalog keys a page uses: its data-i18n attributes and
// the dotted string literals in its script.
var i18nKeys = regexp.MustCompile(`data-i18n(?:-aria|-title|-placeholder)?="([^"{]+)"|data-title-key="([^"]+)"|"([a-z]+\.[a-zA-Z]+)"`)

// i18nElement embeds the catalogs, limited to the given keys when any are
// given, so the CLI delivery page carries only the text it shows.
func i18nElement(keys ...string) string {
	selected := catalogs
	if len(keys) > 0 {
		selected = map[string]map[string]string{}
		for _, language := range languages {
			selected[language] = map[string]string{}
			for _, key := range keys {
				if value, ok := catalogs[language][key]; ok {
					selected[language][key] = value
				}
			}
		}
	}
	// json.Marshal sorts keys and escapes <, >, and &, so the output is
	// deterministic and cannot close the script element.
	encoded, err := json.Marshal(selected)
	if err != nil {
		panic(err)
	}
	return `<script id="tegami-i18n" type="application/json">` + string(encoded) + `</script>`
}

// usedKeys lists the catalog keys referenced by the given page sources.
func usedKeys(sources ...string) []string {
	var keys []string
	for _, source := range sources {
		for _, match := range i18nKeys.FindAllStringSubmatch(source, -1) {
			keys = append(keys, match[1]+match[2]+match[3])
		}
	}
	return keys
}

// text returns the translation of key with {name} placeholders replaced by
// the given name and value pairs.
func text(language, key string, pairs ...string) string {
	template, ok := catalogs[language][key]
	if !ok {
		template = catalogs["en"][key]
	}
	replacements := make([]string, 0, len(pairs))
	for index := 0; index+1 < len(pairs); index += 2 {
		replacements = append(replacements, "{"+pairs[index]+"}", pairs[index+1])
	}
	return strings.NewReplacer(replacements...).Replace(template)
}
