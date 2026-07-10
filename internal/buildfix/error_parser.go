package buildfix

import (
	"strings"

	"github.com/vitorcvargas-code/fabric/internal/shared"
)

func ExtractErrorLines(output string, filters []string) string {
	return shared.ExtractErrorLines(output, filters)
}

func isEmptyOrFormatOnly(output string) bool {
	if output == "" {
		return true
	}
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) != "" {
			return false
		}
	}
	return true
}
