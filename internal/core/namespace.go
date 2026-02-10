package core

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
)

// NormalizeNamespace normalizes a user-provided or config-derived namespace
// string for use in labels and paths. Returns "default" for empty input.
//
// Use this for namespaces that come from CLI flags or profile names.
// For deriving a namespace from a config file path, use NamespaceFromPath
// instead - it produces a hash-based namespace that avoids collisions.
func NormalizeNamespace(ns string) string {
	ns = strings.TrimSpace(strings.ToLower(ns))
	if ns == "" {
		return "default"
	}
	ns = strings.ReplaceAll(ns, " ", "_")
	ns = strings.ReplaceAll(ns, ".", "_")
	return ns
}

// NamespaceFromPath derives a stable, unique namespace from a config
// file path using a SHA1 hash suffix. This prevents collisions when
// multiple config files share the same parent directory name.
func NamespaceFromPath(path string) string {
	clean := filepath.Clean(path)
	h := sha1.Sum([]byte(clean))
	suffix := hex.EncodeToString(h[:])[:10]
	base := sanitizeForNamespace(filepath.Base(filepath.Dir(clean)))
	if base == "" {
		base = "cfg"
	}
	return fmt.Sprintf("%s-%s", base, suffix)
}

func sanitizeForNamespace(in string) string {
	in = strings.ToLower(strings.TrimSpace(in))
	if in == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range in {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('-')
	}
	out := strings.Trim(b.String(), "-_")
	if out == "" {
		return ""
	}
	if len(out) > 32 {
		out = out[:32]
	}
	return out
}
