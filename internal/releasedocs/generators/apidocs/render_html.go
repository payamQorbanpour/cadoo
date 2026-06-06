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
	if err := nodeToJSON(node, &b); err != nil {
		return nil, fmt.Errorf("yamlToJSON: encode: %w", err)
	}
	return []byte(b.String()), nil
}

// nodeToJSON recursively encodes a yaml.Node into the supplied Builder,
// sorting map keys at every MappingNode.
func nodeToJSON(n *yaml.Node, b *strings.Builder) error {
	// Resolve aliases: an AliasNode points to its anchor target.
	if n.Kind == yaml.AliasNode {
		return nodeToJSON(n.Alias, b)
	}

	switch n.Kind {
	case yaml.MappingNode:
		return mappingToJSON(n, b)
	case yaml.SequenceNode:
		return sequenceToJSON(n, b)
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
func mappingToJSON(n *yaml.Node, b *strings.Builder) error {
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
		if err := nodeToJSON(p.val, b); err != nil {
			return err
		}
	}
	b.WriteByte('}')
	return nil
}

// sequenceToJSON encodes a YAML sequence (array) to JSON.
func sequenceToJSON(n *yaml.Node, b *strings.Builder) error {
	b.WriteByte('[')
	for i, child := range n.Content {
		if i > 0 {
			b.WriteByte(',')
		}
		if err := nodeToJSON(child, b); err != nil {
			return err
		}
	}
	b.WriteByte(']')
	return nil
}

// scalarToJSON encodes a YAML scalar to the appropriate JSON literal.
// It respects the YAML tag so "true", "false", "null", and numeric scalars
// are emitted as JSON literals, not as strings.
func scalarToJSON(n *yaml.Node, b *strings.Builder) error {
	// Resolve the effective tag (may be short "!!str" or long-form).
	tag := n.ShortTag()
	switch tag {
	case "!!bool":
		if n.Value == "true" || n.Value == "1" {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case "!!null":
		b.WriteString("null")
	case "!!int", "!!float":
		// Emit the raw numeric value; yaml.v3 has already validated it.
		b.WriteString(n.Value)
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
