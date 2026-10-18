package envelope

import "fmt"

// humanSize formats a byte count with binary units, as the pages do.
func humanSize(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d bytes", size)
	}
	value, unit := float64(size)/1024, "KiB"
	for _, next := range []string{"MiB", "GiB"} {
		if value < 1024 {
			break
		}
		value, unit = value/1024, next
	}
	return fmt.Sprintf("%.1f %s", value, unit)
}
