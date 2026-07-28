package permissions

import (
	"path/filepath"
	"strings"

	"cyber-code/internal/security"
)

// SafeTargetSummary describes the authorization target without exposing raw
// process arguments during startup. Interactive command prompts may request a
// redacted full command so the user can assess its behavior.
func SafeTargetSummary(request Request, includeCommandArguments bool) string {
	if len(request.Network) > 0 {
		return strings.Join(request.Network, ", ")
	}
	if request.Command != "" {
		if includeCommandArguments {
			return security.NewRedactor().Text(request.Command)
		}
		fields := strings.Fields(request.Command)
		if len(fields) > 0 {
			return filepath.Base(strings.Trim(fields[0], `"'`))
		}
	}
	if len(request.Paths) > 0 {
		return strings.Join(request.Paths, "\n")
	}
	return ""
}
