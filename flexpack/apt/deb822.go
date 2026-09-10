package apt

import (
	"bufio"
	"io"
	"strings"
)

// parseDeb822 parses a deb822-formatted stream and returns all stanzas as maps.
// Stanzas are separated by blank lines. Fields are "Key: value" pairs.
// Continuation lines (starting with a space) are skipped — we only need
// single-line fields (Package, Version, Architecture, Filename, SHA256, SHA1, MD5sum).
func parseDeb822(r io.Reader) []map[string]string {
	var stanzas []map[string]string
	current := make(map[string]string)

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1<<20), 1<<20) // 1 MiB buffer for long Description lines

	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			if len(current) > 0 {
				stanzas = append(stanzas, current)
				current = make(map[string]string)
			}
			continue
		}

		// Continuation line — skip, we only need single-line values
		if line[0] == ' ' || line[0] == '\t' {
			continue
		}

		if idx := strings.IndexByte(line, ':'); idx > 0 {
			key := line[:idx]
			value := strings.TrimSpace(line[idx+1:])
			current[key] = value
		}
	}

	// Flush final stanza (file may not end with a blank line)
	if len(current) > 0 {
		stanzas = append(stanzas, current)
	}

	return stanzas
}
