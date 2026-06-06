// Package apidocs (render_markdown.go) implements the deterministic Markdown
// API-reference renderer. It executes a text/template preset over the sorted
// spec model produced by parse.go, escaping spec-derived string fields before
// injection to prevent Markdown-control-character injection (T-03-07, D-06).
package apidocs

import (
	_ "embed"
	"html/template"
	"strings"
	texttemplate "text/template"
)

// mdBackslashReplacer backslash-escapes Markdown-significant characters that
// would alter document structure if inserted verbatim from spec-derived fields.
// It is applied BEFORE html/template.HTMLEscapeString; the HTML layer that
// follows escapes <, >, &, ', " but leaves backslashes intact, so the
// backslash-escaped Markdown syntax is preserved verbatim in the output.
//
// Characters escaped (backslash prefix):
//   - `\` itself — must be first to avoid double-escaping subsequent replacements
//   - `|`  — corrupts pipe-table cell boundaries
//   - “ ` “  — starts code spans / fenced code blocks
//   - `*`  — bold/italic emphasis
//   - `_`  — bold/italic emphasis (underscore form)
//   - `#`  — ATX headings
//   - `[`  — starts link/image syntax
//   - `]`  — ends link/image syntax
//
// Newlines are NOT handled here; they are replaced with `<br>` by escapeField
// AFTER HTML escaping, so the `<br>` markup is emitted literally rather than
// being HTML-escaped to `&lt;br&gt;` (T-03-07).
var mdBackslashReplacer = strings.NewReplacer(
	`\`, `\\`, // must be first
	`|`, `\|`,
	"`", "\\`",
	`*`, `\*`,
	`_`, `\_`,
	`#`, `\#`,
	`[`, `\[`,
	`]`, `\]`,
)

// apidocsPreset is the embedded Markdown preset template (presets/apidocs.tmpl).
// Using []byte avoids a string copy on each use; loaded once at program start.
//
//go:embed presets/apidocs.tmpl
var apidocsPreset []byte

// apiDocsData is the data object passed to the Markdown template.
// All string fields are pre-escaped before the template sees them so that
// spec-sourced values cannot inject Markdown-control characters or HTML
// metacharacters (T-03-07). See escapeField for the escaping applied.
type apiDocsData struct {
	// Title is the API title from info.title, Markdown- and HTML-escaped.
	Title string
	// Version is the spec version string (e.g. "3.0.3"), Markdown- and HTML-escaped.
	Version string
	// Operations is the sorted, flat list of operations extracted from the spec.
	// Iteration is deterministic: sorted by path key (specModel.Paths is already
	// sorted by parse.go via sort.Strings) then by canonical HTTP verb order within
	// each path (GET, PUT, POST, DELETE).
	Operations []mdOperationItem
}

// mdOperationItem is a single HTTP operation for Markdown rendering.
// All string fields are pre-escaped to prevent Markdown-control injection
// and HTML metacharacter injection (T-03-07). See escapeField.
type mdOperationItem struct {
	// Method is the HTTP verb in uppercase (GET, POST, PUT, DELETE).
	Method string
	// Path is the raw path string (e.g. "/pets/{id}"), Markdown- and HTML-escaped.
	Path string
	// Summary is the operation's short summary, Markdown- and HTML-escaped.
	Summary string
	// Description is the operation's full description, Markdown- and HTML-escaped.
	Description string
	// Parameters is the ordered list of parameters in spec source order.
	Parameters []mdParamItem
}

// mdParamItem is a single operation parameter for Markdown rendering.
// All string fields are pre-escaped (T-03-07).
type mdParamItem struct {
	// Name is the parameter name, Markdown- and HTML-escaped.
	Name string
	// In is the parameter location ("query", "path", "header", "body"), Markdown- and HTML-escaped.
	In string
}

// loadMarkdownTemplate parses the embedded apidocs.tmpl preset using
// text/template (Markdown output — NOT html/template, which would auto-escape
// the already-escaped strings a second time).
func loadMarkdownTemplate() (*texttemplate.Template, error) {
	return texttemplate.New("apidocs.tmpl").Parse(string(apidocsPreset))
}

// escapeField sanitises a spec-derived string field before it is passed to the
// text/template renderer.  It applies three steps in order (T-03-07):
//
//  1. Markdown backslash-escaping: backslash-escapes characters that would
//     alter Markdown document structure (|, `, *, _, #, [, ], \).
//     See mdBackslashReplacer for the full replacement table.
//
//  2. HTML metacharacter escaping via html/template.HTMLEscapeString: escapes
//     <, >, &, ', " from the backslash-escaped result so that attacker-
//     controlled HTML cannot be injected when the Markdown is later rendered
//     to HTML by a Markdown processor.
//
//  3. Newline replacement: replaces remaining \n (and \r) with a literal
//     `<br>` so table cells remain single-row. This step runs AFTER HTML
//     escaping so the `<br>` tag is emitted verbatim rather than being
//     HTML-escaped to `&lt;br&gt;`.
//
// Ordering rationale: steps 1 → 2 → 3 ensures that (a) Markdown chars from
// the original spec are backslash-escaped before HTML encoding adds new
// characters (e.g. & in &amp;), (b) HTML chars are encoded before `<br>` is
// inserted so `<br>` is not itself encoded, and (c) backslashes survive HTML
// encoding unchanged (html/template does not escape backslashes).
func escapeField(s string) string {
	// Step 1: Markdown backslash-escape.
	s = mdBackslashReplacer.Replace(s)
	// Step 2: HTML-escape (encodes < > & ' " from the original spec content).
	s = template.HTMLEscapeString(s)
	// Step 3: Replace newlines with <br> (must follow HTML escaping).
	s = strings.ReplaceAll(s, "\n", "<br>")
	s = strings.ReplaceAll(s, "\r", "")
	return s
}

// renderMarkdown renders a deterministic Markdown API reference from the
// parsed spec model.  Spec-derived string fields are Markdown- and HTML-escaped
// before template execution to mitigate injection via untrusted spec content
// (T-03-07). See escapeField for the escaping applied.
//
// Iteration is sorted-deterministic: specModel.Paths is already sorted
// alphabetically by path key (parse.go sort.Strings); parameter order within
// each operation is preserved from the parsed order.
//
// Returns an error only when template parsing or execution fails.
func renderMarkdown(model *specModel) ([]byte, error) {
	tmpl, err := loadMarkdownTemplate()
	if err != nil {
		return nil, err
	}

	data := buildTemplateData(model)

	var b strings.Builder
	if err := tmpl.Execute(&b, data); err != nil {
		return nil, err
	}
	return []byte(b.String()), nil
}

// buildTemplateData converts a specModel into an apiDocsData value suitable
// for template execution.  All spec-derived string fields are escaped here
// so the template itself does not need to call any escape function.
func buildTemplateData(model *specModel) apiDocsData {
	data := apiDocsData{
		Title:   escapeField(model.Title),
		Version: escapeField(model.Version),
	}

	ops := make([]mdOperationItem, 0, len(model.Paths))
	for _, op := range model.Paths {
		params := make([]mdParamItem, 0, len(op.Parameters))
		for _, p := range op.Parameters {
			params = append(params, mdParamItem{
				Name: escapeField(p.Name),
				In:   escapeField(p.In),
			})
		}
		ops = append(ops, mdOperationItem{
			Method:      op.Method, // HTTP verbs are code-generated constants — no escape needed
			Path:        escapeField(op.Path),
			Summary:     escapeField(op.Summary),
			Description: escapeField(op.Description),
			Parameters:  params,
		})
	}
	data.Operations = ops
	return data
}
