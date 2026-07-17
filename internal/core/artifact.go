package core

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// ReadJSON reads path and unmarshals it into v.
func ReadJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// WriteJSON writes v as indented JSON to path, creating parent dirs. Go's
// encoder is BOM-less UTF-8 by default (no per-host BOM tax like PowerShell).
func WriteJSON(path string, v any) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
