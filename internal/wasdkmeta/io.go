package wasdkmeta

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// ErrSchemaMismatch is returned by Read when a .wasdkmeta.json file was written
// by an incompatible IR schema version; re-run ingest to refresh it.
var ErrSchemaMismatch = errors.New("wasdkmeta schema version mismatch")

// Extension is the suffix of a serialized namespace.
const Extension = ".wasdkmeta.json"

// FileName returns the canonical metadata file name for a namespace,
// e.g. "Microsoft.UI.Xaml.Controls.wasdkmeta.json".
func FileName(namespace string) string {
	return namespace + Extension
}

// Write serializes one namespace to dir/<Namespace>.wasdkmeta.json.
//
// Marshalling a map sorts its keys, and every slice in the IR is in metadata
// order, so the output is byte-stable for a given input — which is what lets CI
// assert that regenerating reproduces what is committed.
func Write(dir string, meta *NamespaceMeta) error {
	meta.SchemaVersion = CurrentSchemaVersion
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("wasdkmeta: marshaling %s: %w", meta.Namespace, err)
	}
	data = append(data, '\n')
	path := filepath.Join(dir, FileName(meta.Namespace))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("wasdkmeta: %w", err)
	}
	return nil
}

// Read deserializes one namespace metadata file.
func Read(path string) (*NamespaceMeta, error) {
	meta, err := Decode(path, CurrentSchemaVersion)
	if err != nil {
		return nil, err
	}
	return meta, nil
}

// Decode reads one namespace metadata file, requiring the given schema
// version. It is exported for the external reader, which consumes
// go-bindings-winrt's files: those carry that module's schema version, which
// this module's may move past.
func Decode(path string, wantVersion int) (*NamespaceMeta, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("wasdkmeta: %w", err)
	}
	var meta NamespaceMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("wasdkmeta: parsing %s: %w", path, err)
	}
	if meta.SchemaVersion != wantVersion {
		return nil, fmt.Errorf("%w: %s has version %d, want %d",
			ErrSchemaMismatch, path, meta.SchemaVersion, wantVersion)
	}
	return &meta, nil
}

// ReadAll loads every .wasdkmeta.json in dir, sorted by namespace.
//
// By namespace rather than by file name, which are not the same order: a file
// name puts the dot separator before the letters, so
// Microsoft.Windows.Widgets.Providers sorts ahead of Microsoft.Windows.Widgets.
func ReadAll(dir string) ([]*NamespaceMeta, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*"+Extension))
	if err != nil {
		return nil, fmt.Errorf("wasdkmeta: %w", err)
	}
	namespaces := make([]*NamespaceMeta, 0, len(paths))
	for _, path := range paths {
		meta, err := Read(path)
		if err != nil {
			return nil, err
		}
		namespaces = append(namespaces, meta)
	}
	sort.Slice(namespaces, func(i, j int) bool { return namespaces[i].Namespace < namespaces[j].Namespace })
	return namespaces, nil
}
