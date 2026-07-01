package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

// iconsDir locates the source icon tree relative to this package. Icons are
// authored under frontend/public and copied verbatim into the embedded
// frontend/dist by the Vite build, then served under /assets/mcp-icons/.
const iconsDir = "../../frontend/public/assets/mcp-icons"

// TestToolIconFilesExist guards against a drift between the Go tool→category
// map and the SVG files on disk: every category a tool points at must have a
// matching tools/<category>.svg, or clients get a 404 on the icon.
func TestToolIconFilesExist(t *testing.T) {
	for name, category := range toolIconCategory {
		path := filepath.Join(iconsDir, "tools", category+".svg")
		if _, err := os.Stat(path); err != nil {
			t.Errorf("tool %q → category %q: missing icon file %s", name, category, path)
		}
	}
}

// TestResourceIconFilesExist guards the same drift for resources, whose icon
// file is named after the MCP resource name (including the underscore in
// service_logs).
func TestResourceIconFilesExist(t *testing.T) {
	var names []string
	for _, r := range staticResources {
		names = append(names, r.name)
	}
	for _, r := range resourceTemplates {
		names = append(names, r.name)
	}

	for _, name := range names {
		path := filepath.Join(iconsDir, "resources", name+".svg")
		if _, err := os.Stat(path); err != nil {
			t.Errorf("resource %q: missing icon file %s", name, path)
		}
	}
}
