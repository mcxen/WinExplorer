package forms

import (
	"os"
	"path/filepath"
	"testing"
)

// TestOpenUIDemoPathsExist is the regression for "no such file or directory"
// during OpenUI / schedule demos on machines without ~/Downloads.
func TestOpenUIDemoPathsExist(t *testing.T) {
	for _, id := range ScenePresetIDs() {
		f, ok := Scene(id, nil)
		if !ok {
			t.Fatalf("scene %s", id)
		}
		for _, field := range f.Fields {
			if field.Kind != KindPath || field.Value == "" {
				continue
			}
			if _, err := os.Stat(field.Value); err != nil {
				t.Fatalf("scene %s field %s=%q: %v", id, field.ID, field.Value, err)
			}
		}
	}
	start := filepath.Join(PreferPath("Downloads"), "missing-child")
	dir := ExistingDir(start)
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("ExistingDir(%q)=%q: %v", start, dir, err)
	}
}
