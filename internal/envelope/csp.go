package envelope

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
)

// Markers in the HTML assets. They include their tags so that escaped page
// content, such as a filename, can never match one.
const (
	cspMarker    = `content="__TEGAMI_CSP__"`
	styleMarker  = `<style>__TEGAMI_STYLE__</style>`
	scriptMarker = `<script>__TEGAMI_SCRIPT__</script>`
)

// contentSecurityPolicy allows exactly the given inline script and style by
// hash. Pages are static files, so a nonce would be public and add nothing;
// hashes keep generated pages byte-identical and reproducible.
func contentSecurityPolicy(script, style string) string {
	return "default-src 'none'; script-src " + inlineHash(script) + " 'wasm-unsafe-eval'; style-src " + inlineHash(style) + "; connect-src 'none'; img-src data:; form-action 'none'; base-uri 'none'"
}

func inlineHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
}

// assemblePage inserts the script, style, and their policy into a page. It
// runs after any template rendering, because html/template rewrites inline
// CSS and JavaScript, which would break their hashes.
func assemblePage(page, script, style string) string {
	page = strings.Replace(page, scriptMarker, wasmElement()+"\n"+scriptMarker, 1)
	page = strings.Replace(page, cspMarker, `content="`+contentSecurityPolicy(script, style)+`"`, 1)
	page = strings.Replace(page, styleMarker, "<style>"+style+"</style>", 1)
	return strings.Replace(page, scriptMarker, "<script>"+script+"</script>", 1)
}
