package envelope

import (
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"html/template"
	"strings"
)

// The browser pages are plain HTML, CSS, and vanilla JavaScript in assets/,
// embedded at build time. Each page has exactly one inline <style> and one
// executable inline <script>, allowed by hash in its Content-Security-Policy.
//
//go:embed assets
var assets embed.FS

func asset(name string) string {
	content, err := assets.ReadFile("assets/" + name)
	if err != nil {
		panic(err)
	}
	return string(content)
}

// studioPage is the complete Studio. It is identical on every build, so a
// published tegami.html can be checked by rebuilding it.
var studioPage = assemblePage(
	strings.NewReplacer(
		codenamesMarker, `<script id="tegami-codenames" type="application/json">`+asset("codenames.json")+`</script>`,
		i18nMarker, i18nElement(),
	).Replace(asset("studio.html")),
	cryptoScript+asset("menu.js")+asset("studio.js"),
	asset("base.css")+asset("studio.css"),
)

// codenamesMarker is replaced with the codename wordlist, which the Studio
// reads as data to name the deliveries it creates.
const codenamesMarker = `<script id="tegami-codenames" type="application/json">__TEGAMI_CODENAMES__</script>`

// The CLI's delivery page deliberately does not name Tegami; see AGENTS.md.
var (
	pageTemplate = template.Must(template.New("page").Parse(asset("page.html")))
	pageScript   = cryptoScript + asset("menu.js") + asset("page.js")
	pageStyle    = asset("base.css") + asset("page.css")
	// The intro key is chosen when the page is rendered, so it is listed here.
	pageCatalog = i18nElement(usedKeys(asset("page.html"), asset("page.js"))...)
)

// cryptoScript is the shared crypto code with the SHA-256 of argon2.wasm filled
// in. It checks the embedded module against that digest before compiling it.
var cryptoScript = func() string {
	sum := sha256.Sum256([]byte(asset("argon2.wasm")))
	return strings.Replace(asset("crypto.js"), "__TEGAMI_ARGON2_SHA256__", hex.EncodeToString(sum[:]), 1)
}()

func wasmElement() string {
	return `<script id="tegami-argon2" type="application/octet-stream">` + base64.StdEncoding.EncodeToString([]byte(asset("argon2.wasm"))) + `</script>`
}
