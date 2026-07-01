package models

import (
	"fmt"
	"strings"
)

// GenerateMD renders a MODELS.md markdown document from the given manifests.
// The result is suitable for committing in the journal repo root so model
// upgrades appear as visible diffs rather than silent drift.
func GenerateMD(manifests []Manifest) string {
	var sb strings.Builder
	sb.WriteString("# Models\n\n")
	sb.WriteString("Managed by `journal models pull`. Re-run that command to refresh this file.\n\n")
	if len(manifests) == 0 {
		sb.WriteString("_No models installed yet. Run `journal models pull` to download the configured model._\n")
		return sb.String()
	}
	sb.WriteString("| Model ID | Revision | SHA-256 Checksum | Gated |\n")
	sb.WriteString("| --- | --- | --- | --- |\n")
	for _, m := range manifests {
		gated := "no"
		if m.Gated {
			gated = fmt.Sprintf("yes — [accept terms](%s)", m.AcceptURL)
		}
		_, _ = fmt.Fprintf(&sb, "| `%s` | `%s` | `%s` | %s |\n", m.ModelID, m.Revision, m.Checksum, gated)
	}
	return sb.String()
}
