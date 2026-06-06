// Package apidocs (render_html.go) implements the self-contained offline Redoc
// HTML renderer. The Redoc standalone bundle is embedded at compile time (via
// the embed directive) so the generated HTML requires no CDN or network access
// (D-05). Output is byte-identical given the same spec bytes and bundle
// (deterministic).
package apidocs

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"gopkg.in/yaml.v3"
)

// redocBundle is the Redoc v2.5.3 standalone JavaScript bundle, embedded at
// compile time. Using []byte avoids a string copy on each use.
//
//go:embed assets/redoc.standalone.js
var redocBundle []byte

// buildRedocHTML constructs a fully self-contained Redoc API-reference HTML
// page from the raw YAML spec bytes and the vendored Redoc bundle.
//
// The spec is converted to sorted-key JSON (no Go-map non-determinism) and
// inlined into a <script> block alongside the embedded bundle. The resulting
// HTML has no external src= references and no cdn.redoc.ly (D-05, T-03-03).
// Two calls with identical (specBytes, bundle) produce byte-identical output
// (D-05 determinism requirement).
//
// Returns an error only when the YAML→JSON conversion fails (malformed spec).
func buildRedocHTML(specBytes []byte, bundle []byte) ([]byte, error) {
	specJSON, err := yamlToJSON(specBytes)
	if err != nil {
		return nil, fmt.Errorf("apidocs: yamlToJSON: %w", err)
	}

	// htmlTemplate is the fixed-layout HTML wrapper.  The two %s format
	// directives receive the bundle bytes and the sorted-key spec JSON
	// respectively.  No timestamps, no random IDs — output is deterministic.
	const htmlTemplate = `<!DOCTYPE html>
<html>
  <head>
    <title>API Reference</title>
    <meta charset="utf-8"/>
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <style>body{margin:0;padding:0;}</style>
  </head>
  <body>
    <div id="redoc-container"></div>
    <script>%s</script>
    <script>Redoc.init(%s,{},document.getElementById('redoc-container'))</script>
  </body>
</html>`

	html := fmt.Sprintf(htmlTemplate, bundle, specJSON)
	return []byte(html), nil
}

// maxNodeDepth is the maximum recursion depth allowed in nodeToJSON.
// This prevents a stack overflow from a deeply nested (or self-referential)
// YAML document. libopenapi rejects recursive anchors before this code is
// reached in normal operation, but this guard provides independent defense-in-
// depth so buildRedocHTML is safe to call from any context (WR-01).
const maxNodeDepth = 1000

// yamlToJSON converts YAML bytes to JSON bytes with map keys sorted
// lexicographically at every depth level.  This eliminates Go-map
// non-determinism (Pitfall 2) so the HTML output is byte-identical
// across runs given the same input spec.
//
// Implementation: decode to a yaml.Node (which preserves YAML structure
// without using map[string]any), then walk the node tree recursively while
// emitting JSON with sorted keys.  Do NOT use json.Marshal(map[string]any{…})
// — that is non-deterministic.
func yamlToJSON(specBytes []byte) ([]byte, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(specBytes, &root); err != nil {
		return nil, fmt.Errorf("yamlToJSON: unmarshal: %w", err)
	}

	// yaml.Unmarshal wraps the decoded tree in a DocumentNode; unwrap it.
	node := &root
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		node = node.Content[0]
	}

	var b strings.Builder
	if err := nodeToJSON(node, &b, 0); err != nil {
		return nil, fmt.Errorf("yamlToJSON: encode: %w", err)
	}
	return []byte(b.String()), nil
}

// nodeToJSON recursively encodes a yaml.Node into the supplied Builder,
// sorting map keys at every MappingNode.
//
// depth tracks the current recursion level. An error is returned when depth
// exceeds maxNodeDepth, providing an independent stack-overflow guard (WR-01)
// that does not rely on libopenapi's anchor/alias validation running first.
func nodeToJSON(n *yaml.Node, b *strings.Builder, depth int) error {
	if depth > maxNodeDepth {
		return fmt.Errorf("nodeToJSON: document exceeds maximum nesting depth %d", maxNodeDepth)
	}

	// Resolve aliases: an AliasNode points to its anchor target.
	if n.Kind == yaml.AliasNode {
		return nodeToJSON(n.Alias, b, depth+1)
	}

	switch n.Kind {
	case yaml.MappingNode:
		return mappingToJSON(n, b, depth)
	case yaml.SequenceNode:
		return sequenceToJSON(n, b, depth)
	case yaml.ScalarNode:
		return scalarToJSON(n, b)
	default:
		// Unknown node kind — emit null as a safe fallback.
		b.WriteString("null")
		return nil
	}
}

// mappingToJSON encodes a YAML mapping (object) to JSON with sorted keys.
// YAML mappings store key-value pairs as adjacent elements in Content:
//
//	Content[0] = key₀, Content[1] = value₀, Content[2] = key₁, …
func mappingToJSON(n *yaml.Node, b *strings.Builder, depth int) error {
	// Collect (key-string, value-node) pairs.
	type kv struct {
		key string
		val *yaml.Node
	}
	pairs := make([]kv, 0, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		keyNode := n.Content[i]
		valNode := n.Content[i+1]
		pairs = append(pairs, kv{key: keyNode.Value, val: valNode})
	}

	// Sort by key — lexicographic ascending (deterministic, D-05 / T-03-08).
	// Simple insertion sort is fine for the small key counts in a spec object.
	for i := 1; i < len(pairs); i++ {
		for j := i; j > 0 && pairs[j].key < pairs[j-1].key; j-- {
			pairs[j], pairs[j-1] = pairs[j-1], pairs[j]
		}
	}

	b.WriteByte('{')
	for i, p := range pairs {
		if i > 0 {
			b.WriteByte(',')
		}
		// Encode the key as a JSON string (handles special characters).
		keyJSON, err := json.Marshal(p.key)
		if err != nil {
			return fmt.Errorf("mappingToJSON: marshal key %q: %w", p.key, err)
		}
		b.Write(keyJSON)
		b.WriteByte(':')
		if err := nodeToJSON(p.val, b, depth+1); err != nil {
			return err
		}
	}
	b.WriteByte('}')
	return nil
}

// sequenceToJSON encodes a YAML sequence (array) to JSON.
func sequenceToJSON(n *yaml.Node, b *strings.Builder, depth int) error {
	b.WriteByte('[')
	for i, child := range n.Content {
		if i > 0 {
			b.WriteByte(',')
		}
		if err := nodeToJSON(child, b, depth+1); err != nil {
			return err
		}
	}
	b.WriteByte(']')
	return nil
}

// scalarToJSON encodes a YAML scalar to the appropriate JSON literal.
// It respects the YAML tag so "true", "false", "null", and numeric scalars
// are emitted as JSON literals, not as strings.
//
// For !!int and !!float, the raw n.Value is NOT written directly: YAML's
// number grammar is a superset of JSON's (hex 0xAF, octal 0o17, binary 0b101,
// underscores 1_000, .inf, -.inf, .nan are valid YAML but invalid JSON).
// Instead, we decode through yaml.Node.Decode to obtain the Go numeric value,
// then re-encode through encoding/json so the output is always valid JSON
// (CR-01, D-05 deterministic-valid-output guarantee).
//
// Integer precision is preserved: integer-tagged scalars are decoded into int64
// first; only values that do not fit int64 fall back to float64.
//
// Special float values (.inf / .nan) are not representable as JSON numbers; they
// are emitted as JSON strings so the page still parses rather than silently
// writing invalid JSON.
func scalarToJSON(n *yaml.Node, b *strings.Builder) error {
	// Resolve the effective tag (may be short "!!str" or long-form).
	tag := n.ShortTag()
	switch tag {
	case "!!bool":
		// yaml.v3 normalises !!bool values to "true" or "false".
		if n.Value == "true" {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case "!!null":
		b.WriteString("null")
	case "!!int":
		// Decode into int64 to preserve integer precision (avoids float64 loss
		// for large integers such as 2^53+1 that cannot be represented exactly).
		var iv int64
		if err := n.Decode(&iv); err != nil {
			// Fallback: try float64 (handles large unsigned ints near MaxInt64).
			var fv float64
			if err2 := n.Decode(&fv); err2 != nil {
				// Last resort: emit as a JSON string rather than invalid JSON.
				enc, _ := json.Marshal(n.Value)
				b.Write(enc)
				return nil
			}
			enc, err2 := json.Marshal(fv)
			if err2 != nil {
				return fmt.Errorf("scalarToJSON: marshal int as float %q: %w", n.Value, err2)
			}
			b.Write(enc)
			return nil
		}
		enc, err := json.Marshal(iv)
		if err != nil {
			return fmt.Errorf("scalarToJSON: marshal int %q: %w", n.Value, err)
		}
		b.Write(enc)
	case "!!float":
		// Decode into float64 through yaml.v3 so that non-JSON forms (hex,
		// octal, underscores) are normalised to their numeric value.
		var fv float64
		if err := n.Decode(&fv); err != nil {
			// Cannot decode (shouldn't happen for !!float) — emit as string.
			enc, _ := json.Marshal(n.Value)
			b.Write(enc)
			return nil
		}
		// JSON does not support Inf or NaN — emit them as strings so the page
		// still parses instead of silently producing invalid JSON.
		if math.IsInf(fv, 0) || math.IsNaN(fv) {
			enc, _ := json.Marshal(n.Value) // emit original YAML text as string
			b.Write(enc)
			return nil
		}
		enc, err := json.Marshal(fv)
		if err != nil {
			return fmt.Errorf("scalarToJSON: marshal float %q: %w", n.Value, err)
		}
		b.Write(enc)
	default:
		// All other scalars (strings, timestamps, etc.) → JSON string.
		s, err := json.Marshal(n.Value)
		if err != nil {
			return fmt.Errorf("scalarToJSON: marshal %q: %w", n.Value, err)
		}
		b.Write(s)
	}
	return nil
}
