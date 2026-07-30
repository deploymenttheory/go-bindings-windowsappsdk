package pipeline

import (
	"bufio"
	"os"
	"strings"
)

// ReadRootsFile reads a committed root-namespace list: one full namespace per
// line, with # comments and blank lines ignored.
//
// The list is explicit rather than "emit everything loaded" so that a metadata
// update adding or removing a namespace shows up as a diff in this file, to be
// reviewed, instead of silently changing the shape of the published API.
func ReadRootsFile(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var roots []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		roots = append(roots, line)
	}
	return roots, scanner.Err()
}
