package envelope

// WriteStudio writes the offline Studio page. An HTML delivery made in the
// Studio is a copy of the Studio's own markup with the encrypted package
// embedded and body[data-mode=envelope] set, which shows only the unbranded
// unlock view.
func WriteStudio(outputPath string) error {
	return writeOutput(outputPath, []byte(studioPage))
}
