package install

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"aidw/cmd/aidw/internal/util"
)

// MergeSettings deep-merges the JSON template at templatePath into the
// settings file at settingsPath.
//
// Merge rules (same as the Python implementation):
//   - Objects: recurse
//   - Arrays: deduplicate by JSON-serialised key (union)
//   - Scalars: user value wins — incoming template does not overwrite
//
// If settingsPath contains invalid JSON it is backed up and a fresh merge
// (template only) is written.
func MergeSettings(settingsPath, templatePath string) error {
	templateData, err := os.ReadFile(templatePath)
	if err != nil {
		return fmt.Errorf("read template: %w", err)
	}
	var tmpl map[string]any
	if err := json.Unmarshal(templateData, &tmpl); err != nil {
		return fmt.Errorf("parse template: %w", err)
	}

	var existing map[string]any
	if data, err := os.ReadFile(settingsPath); err == nil {
		if jerr := json.Unmarshal(data, &existing); jerr != nil {
			backup := backupPath(settingsPath)
			if rerr := os.Rename(settingsPath, backup); rerr != nil {
				return fmt.Errorf("backup invalid settings: %w", rerr)
			}
			fmt.Fprintf(os.Stderr,
				"WARNING: %s contains invalid JSON (%v). Backed up to %s and starting fresh.\n",
				settingsPath, jerr, backup)
			existing = map[string]any{}
		}
	} else if os.IsNotExist(err) {
		existing = map[string]any{}
	} else {
		return fmt.Errorf("read settings: %w", err)
	}

	merged := mergeDict(existing, tmpl)
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return err
	}
	out, _ := json.MarshalIndent(merged, "", "  ")
	return util.AtomicWrite(settingsPath, append(out, '\n'), 0o644)
}

func mergeDict(existing, incoming map[string]any) map[string]any {
	out := make(map[string]any, len(existing))
	for k, v := range existing {
		out[k] = v
	}
	for k, v := range incoming {
		if cur, ok := out[k]; ok {
			curMap, curIsMap := cur.(map[string]any)
			inMap, inIsMap := v.(map[string]any)
			curSlice, curIsSlice := cur.([]any)
			inSlice, inIsSlice := v.([]any)
			switch {
			case curIsMap && inIsMap:
				out[k] = mergeDict(curMap, inMap)
			case curIsSlice && inIsSlice:
				out[k] = mergeLists(curSlice, inSlice)
			// scalar: user value wins — do not overwrite
			}
		} else {
			out[k] = v
		}
	}
	return out
}

func mergeLists(existing, incoming []any) []any {
	seen := map[string]bool{}
	var result []any
	for _, item := range append(existing, incoming...) {
		key, _ := json.Marshal(item)
		k := string(key)
		if !seen[k] {
			seen[k] = true
			result = append(result, item)
		}
	}
	return result
}

func backupPath(p string) string {
	base := p[:len(p)-len(filepath.Ext(p))]
	bak := base + ".json.bak"
	if _, err := os.Stat(bak); err != nil {
		return bak
	}
	ts := time.Now().UTC().Format("20060102150405")
	return base + ".json." + ts + ".bak"
}
