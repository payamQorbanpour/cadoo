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

// apidocsPreset is the embedded Markdown preset template (presets/apidocs.tmpl).
// Using []byte avoids a string copy on each use; loaded once at program start.
//
//go:embed presets/apidocs.tmpl
var apidocsPreset []byte

// apiDocsData is the data object passed to the Markdown template.
// All string fields are pre-escaped before the template sees them so that
// spec-sourced values cannot inject Markdown-control characters (T-03-07).
type apiDocsData struct {
	// Title is the API title from info.title, HTML-escaped.
	Title string
	// Version is the spec version string (e.g. "3.0.3"), HTML-escaped.
	Version string
	// Operations is the sorted, flat list of operations extracted from the spec.
	// Iteration is deterministic: sorted by path key (specModel.Paths is already
	// sorted by parse.go via sort.Strings) then by canonical HTTP verb order within
	// each path (GET, PUT, POST, DELETE).
	Operations []mdOperationItem
}

// mdOperationItem is a single HTTP operation for Markdown rendering.
// All string fields are pre-escaped to prevent Markdown-control injection.
type mdOperationItem struct {
	// Method is the HTTP verb in uppercase (GET, POST, PUT, DELETE).
	Method string
	// Path is the raw path string (e.g. "/pets/{id}"), HTML-escaped.
	Path string
	// Summary is the operation's short summary, HTML-escaped.
	Summary string
	// Description is the operation's full description, HTML-escaped.
	Description string
	// Parameters is the sorted list of parameters for this operation.
	Parameters []mdParamItem
}

// mdParamItem is a single operation parameter for Markdown rendering.
// All string fields are pre-escaped.
type mdParamItem struct {
	// Name is the parameter name, HTML-escaped.
	Name string
	// In is the parameter location ("query", "path", "header", "body"), HTML-escaped.
	In string
}

// loadMarkdownTemplate parses the embedded apidocs.tmpl preset using
// text/template (Markdown output — NOT html/template, which would auto-escape
// the already-escaped strings a second time).
func loadMarkdownTemplate() (*texttemplate.Template, error) {
	return texttemplate.New("apidocs.tmpl").Parse(string(apidocsPreset))
}

// escapeField applies HTML escaping to a spec-derived string field before it
// is passed to the text/template renderer.  text/template does NOT auto-escape,
// so this manual step is required to prevent Markdown-control injection from
// untrusted spec content (T-03-07, RESEARCH § Security Domain).
func escapeField(s string) string {
	return string(template.HTMLEscapeString(s))
}

// renderMarkdown renders a deterministic Markdown API reference from the
// parsed spec model.  Spec-derived string fields are HTML-escaped before
// template execution to mitigate injection via untrusted spec content (T-03-07).
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
