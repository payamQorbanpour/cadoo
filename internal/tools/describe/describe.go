// Package describe implements /describe — propose a clearer PR title and body.
package describe

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/payamqorbanpour/cadoo/internal/tools"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

// filesIcon is rendered next to the File Walkthrough header.
const filesIcon = `<img src="https://raw.githubusercontent.com/payamqorbanpour/cadoo/main/docs/assets/Magnifier.png" height="20" align="absmiddle" alt="Walkthrough">`

// risksIcon is rendered next to the Risks header.
const risksIcon = `<img src="https://raw.githubusercontent.com/payamqorbanpour/cadoo/main/docs/assets/Risk.png" height="20" align="absmiddle" alt="Risks">`

// labelEnhancement and siblings are the fixed walkthrough categories. Order
// here is the render order in the table. "Additional files" is the catch-all
// for files the LLM did not label.
const (
	labelEnhancement = "Enhancement"
	labelBugFix      = "Bug fix"
	labelTests       = "Tests"
	labelDocs        = "Documentation"
	labelConfig      = "Configuration changes"
	labelFormatting  = "Formatting"
	labelAdditional  = "Additional files"
)

var walkthroughOrder = []string{
	labelEnhancement,
	labelBugFix,
	labelTests,
	labelDocs,
	labelConfig,
	labelFormatting,
	labelAdditional,
}

const systemPrompt = `You are Cadoo. Propose a concise, reviewer-friendly description for this pull request.

Respond with ONLY a JSON object:
{
  "title":   "<≤70-char imperative-mood title>",
  "intent":  "<one-sentence summary of what this PR does and why>",
  "type":    "<comma-separated labels: Bug fix | Enhancement | Refactor | Tests | Docs | Chore>",
  "changes": [ "<short bullet — one per meaningful change, ≤90 chars>" ],
  "risks":   [ "<short bullet — one per risk, ≤90 chars; empty array if low-risk>" ],
  "walkthrough": [
    {
      "path":        "<file path exactly as listed under ## Diff>",
      "label":       "<one of: Enhancement | Bug fix | Tests | Documentation | Configuration changes | Formatting>",
      "description": "<≤90-char summary of what changed in this file, imperative mood>"
    }
  ]
}

Rules:
- changes: 2-6 bullets max. Skip trivial moves.
- walkthrough: one entry per file actually present in the diff. Use the exact path. Skip files you have no meaningful description for — they will be grouped under "Additional files" automatically.
- Label rules: pick the single best label per file. Tests = *_test.* or files under testdata/; Configuration changes = .yaml/.yml/.toml/.json/.ini/Dockerfile/CI workflow files; Documentation = .md/.rst/docs/**; Formatting = pure whitespace/style with no behaviour change. Otherwise Enhancement (or Bug fix if the PR is fixing a defect).
- Do not invent files or behaviour not in the diff.
- Keep every field tight — the reader skims this.`

// WalkthroughFile is one row in the File Walkthrough table.
type WalkthroughFile struct {
	Path        string `json:"path"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// Output is the structured response.
type Output struct {
	Title       string            `json:"title"`
	Intent      string            `json:"intent"`
	Type        string            `json:"type"`
	Changes     []string          `json:"changes"`
	Risks       []string          `json:"risks"`
	Walkthrough []WalkthroughFile `json:"walkthrough"`
}

// Tool implements tools.Tool.
type Tool struct{}

// Name implements tools.Tool.
func (Tool) Name() string { return "describe" }

// Run implements tools.Tool. Edits the PR body in place: the user's original
// description stays on top, Cadoo's section is appended (and replaced in
// place on subsequent dispatches via the marker pair the orchestrator
// recognises).
func (Tool) Run(ctx context.Context, in tools.Input) (*tools.Result, error) {
	user := tools.BuildDiffPrompt(in, tools.PromptOptions{SkipStaticAnalysis: true})
	var out Output
	sys := tools.EffectivePrompt("describe", systemPrompt, in.Config)
	if err := tools.CallJSON(ctx, in.LLM, in.Model, sys, user, &out); err != nil {
		return nil, err
	}
	body := buildSection(out, in.Files, true)
	return &tools.Result{
		EditPRBody: &body,
	}, nil
}

func buildSection(o Output, files []vcs.FileChange, withImage bool) string {
	var b strings.Builder
	if o.Title != "" {
		b.WriteString("**Title:** ")
		b.WriteString(o.Title)
		b.WriteString("\n\n")
	}
	if o.Intent != "" {
		b.WriteString(o.Intent)
		b.WriteString("\n\n")
	}
	if o.Type != "" {
		b.WriteString("**Type:** ")
		b.WriteString(o.Type)
		b.WriteString("\n\n")
	}
	if len(o.Changes) > 0 {
		b.WriteString("**Changes**\n\n")
		for _, c := range o.Changes {
			c = strings.TrimSpace(c)
			if c == "" {
				continue
			}
			b.WriteString("- ")
			b.WriteString(c)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	if risks := nonEmpty(o.Risks); len(risks) > 0 {
		b.WriteString(risksIcon)
		b.WriteString(" **Risks**\n\n")
		for _, r := range risks {
			b.WriteString("- ")
			b.WriteString(r)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	if walk := renderWalkthrough(o.Walkthrough, files, withImage); walk != "" {
		b.WriteString(walk)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// renderWalkthrough produces the File Walkthrough section: an HTML table
// where each row is a category and the second cell collapses to show file
// names, descriptions, and +adds/-deletes counts. Files the LLM did not
// label (or labelled with something we don't recognise) fall into
// "Additional files".
func renderWalkthrough(items []WalkthroughFile, files []vcs.FileChange, withImage bool) string {
	if len(files) == 0 {
		return ""
	}
	stats := make(map[string]vcs.FileChange, len(files))
	for _, f := range files {
		stats[f.Path] = f
	}
	descByPath := make(map[string]WalkthroughFile, len(items))
	for _, w := range items {
		if w.Path == "" {
			continue
		}
		if _, ok := stats[w.Path]; !ok {
			continue
		}
		descByPath[w.Path] = w
	}
	buckets := make(map[string][]WalkthroughFile, len(walkthroughOrder))
	for _, f := range files {
		w, labeled := descByPath[f.Path]
		label := canonicalLabel(w.Label)
		if !labeled || label == "" {
			label = labelAdditional
		}
		buckets[label] = append(buckets[label], WalkthroughFile{
			Path:        f.Path,
			Label:       label,
			Description: strings.TrimSpace(w.Description),
		})
	}
	for k := range buckets {
		sort.Slice(buckets[k], func(i, j int) bool {
			return buckets[k][i].Path < buckets[k][j].Path
		})
	}

	var b strings.Builder
	b.WriteString("<details><summary>")
	if withImage {
		b.WriteString(filesIcon)
		b.WriteString(" ")
	}
	b.WriteString("<strong>File Walkthrough</strong></summary>\n\n")
	b.WriteString("<table>\n<thead><tr><th></th><th>Relevant files</th></tr></thead>\n<tbody>\n")
	for _, label := range walkthroughOrder {
		rows := buckets[label]
		if len(rows) == 0 {
			continue
		}
		b.WriteString("<tr>\n<td><strong>")
		b.WriteString(label)
		b.WriteString("</strong></td>\n<td>\n")
		fmt.Fprintf(&b, "<details><summary>%d files</summary>\n\n", len(rows))
		b.WriteString("<table>\n")
		for _, r := range rows {
			f := stats[r.Path]
			desc := r.Description
			if desc == "" {
				desc = "—"
			}
			b.WriteString("<tr>\n<td>\n<strong>")
			b.WriteString(htmlEscape(displayName(r.Path)))
			b.WriteString("</strong><br>\n<code>")
			b.WriteString(htmlEscape(desc))
			b.WriteString("</code>\n</td>\n<td>")
			fmt.Fprintf(&b, "+%d/-%d", f.Additions, f.Deletions)
			b.WriteString("</td>\n</tr>\n")
		}
		b.WriteString("</table>\n\n</details>\n</td>\n</tr>\n")
	}
	b.WriteString("</tbody>\n</table>\n\n</details>\n")
	return b.String()
}

// canonicalLabel maps a raw LLM-supplied label to the fixed set. Returns ""
// when there is no usable match — the caller treats that as "Additional
// files".
func canonicalLabel(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	switch s {
	case "enhancement", "feature", "feat":
		return labelEnhancement
	case "bug fix", "bugfix", "fix", "bug":
		return labelBugFix
	case "tests", "test":
		return labelTests
	case "documentation", "docs", "doc":
		return labelDocs
	case "configuration changes", "configuration", "config":
		return labelConfig
	case "formatting", "format", "style":
		return labelFormatting
	case "additional files", "additional", "other":
		return labelAdditional
	}
	return ""
}

func nonEmpty(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		out = append(out, s)
	}
	return out
}

func displayName(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

func htmlEscape(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
	)
	return r.Replace(s)
}
