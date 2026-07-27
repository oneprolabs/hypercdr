package veleroassets

import (
	"embed"
	"io/fs"
	"sort"
)

//go:embed crds/*.yaml
var crdFS embed.FS

func CRDsYAML() ([]byte, error) {
	entries, err := fs.ReadDir(crdFS, "crds")
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})
	out := []byte{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := crdFS.ReadFile("crds/" + entry.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, []byte("---\n")...)
		out = append(out, data...)
		if len(out) == 0 || out[len(out)-1] != '\n' {
			out = append(out, '\n')
		}
	}
	return out, nil
}
