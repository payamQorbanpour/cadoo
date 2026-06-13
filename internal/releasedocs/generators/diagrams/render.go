package diagrams

import (
	"bytes"
	"path"
	"strings"
)

// wrapMermaidFence wraps the raw Mermaid source bytes in a fixed ```mermaid
// markdown fenced code block. The body's trailing newlines are normalized
// (bytes.TrimRight) and exactly one trailing newline is emitted after the
// closing fence, so the output is byte-identical across re-runs given the same
// source (D-05/D-06, golden-file testable). It uses a fixed bytes.Buffer — no
// text/template and no embed — because the only variable is the source body.
func wrapMermaidFence(src []byte) []byte {
	body := bytes.TrimRight(src, "\n")
	var b bytes.Buffer
	b.WriteString("```mermaid\n")
	b.Write(body)
	b.WriteString("\n```\n")
	return b.Bytes()
}

// diagramName derives a published filename segment from a config-supplied
// source path. It uses path.Base ONLY (stripping all directory components) and
// then strips a trailing ".mmd" or ".mermaid" extension, so the returned name
// can never contain "../" or escape the artifact directory (Pitfall 4, T-07-03).
func diagramName(srcPath string) string {
	base := path.Base(srcPath)
	base = strings.TrimSuffix(base, ".mmd")
	base = strings.TrimSuffix(base, ".mermaid")
	return base
}
