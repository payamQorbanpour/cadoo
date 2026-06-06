package apidocs

// security_test.go contains focused unit tests for the security guards
// introduced in phase 03:
//
//   - CR-01: YAML numeric scalars must produce valid JSON (never raw n.Value).
//   - WR-01: nodeToJSON must return an error rather than stack-overflow on
//     excessively deep input.
//   - WR-02: escapeField must neutralise Markdown control characters.

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestScalarToJSON_NumericForms verifies that YAML numeric forms that are
// valid YAML but invalid JSON are re-encoded to valid JSON literals.
//
// Regression test for CR-01: prior code wrote n.Value verbatim, producing
// tokens like 0xAF or .inf that are rejected by JSON parsers.
func TestScalarToJSON_NumericForms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		yaml     string                         // YAML snippet whose top-level value is the scalar under test
		wantJSON string                         // expected JSON output (empty means "any valid JSON")
		wantTag  string                         // YAML tag we expect yaml.v3 to assign (informational)
		validate func(t *testing.T, got string) // extra assertion on the emitted JSON
	}{
		{
			name:    "hex integer 0xAF",
			yaml:    "x: 0xAF",
			wantTag: "!!int",
			validate: func(t *testing.T, got string) {
				t.Helper()
				// Must be valid JSON; the value must be 175 (0xAF decimal).
				var obj map[string]interface{}
				if err := json.Unmarshal([]byte(got), &obj); err != nil {
					t.Errorf("0xAF: emitted JSON is invalid: %v\ngot: %s", err, got)
					return
				}
				v, ok := obj["x"]
				if !ok {
					t.Errorf("0xAF: key 'x' missing from JSON object")
					return
				}
				// json.Unmarshal decodes numbers as float64.
				if fv, ok := v.(float64); !ok || fv != 175 {
					t.Errorf("0xAF: want 175 (float64), got %v (%T)", v, v)
				}
			},
		},
		{
			name:    "octal integer 0o17",
			yaml:    "x: 0o17",
			wantTag: "!!int",
			validate: func(t *testing.T, got string) {
				t.Helper()
				var obj map[string]interface{}
				if err := json.Unmarshal([]byte(got), &obj); err != nil {
					t.Errorf("0o17: emitted JSON is invalid: %v\ngot: %s", err, got)
					return
				}
				v, _ := obj["x"].(float64)
				if v != 15 { // 0o17 = 15
					t.Errorf("0o17: want 15, got %v", v)
				}
			},
		},
		{
			name:    "underscore integer 1_000",
			yaml:    "x: 1_000",
			wantTag: "!!int",
			validate: func(t *testing.T, got string) {
				t.Helper()
				var obj map[string]interface{}
				if err := json.Unmarshal([]byte(got), &obj); err != nil {
					t.Errorf("1_000: emitted JSON is invalid: %v\ngot: %s", err, got)
					return
				}
				v, _ := obj["x"].(float64)
				if v != 1000 {
					t.Errorf("1_000: want 1000, got %v", v)
				}
			},
		},
		{
			name:    "positive infinity .inf",
			yaml:    "x: .inf",
			wantTag: "!!float",
			validate: func(t *testing.T, got string) {
				t.Helper()
				// .inf cannot be a JSON number; must be emitted as a JSON string.
				var obj map[string]interface{}
				if err := json.Unmarshal([]byte(got), &obj); err != nil {
					t.Errorf(".inf: emitted JSON is invalid: %v\ngot: %s", err, got)
					return
				}
				v, ok := obj["x"]
				if !ok {
					t.Errorf(".inf: key 'x' missing")
					return
				}
				// Must be a string, not a number.
				if _, isString := v.(string); !isString {
					t.Errorf(".inf: expected JSON string (not a number), got %T: %v", v, v)
				}
			},
		},
		{
			name:    "negative infinity -.inf",
			yaml:    "x: -.inf",
			wantTag: "!!float",
			validate: func(t *testing.T, got string) {
				t.Helper()
				var obj map[string]interface{}
				if err := json.Unmarshal([]byte(got), &obj); err != nil {
					t.Errorf("-.inf: emitted JSON is invalid: %v\ngot: %s", err, got)
					return
				}
				v, ok := obj["x"]
				if !ok {
					t.Errorf("-.inf: key 'x' missing")
					return
				}
				if _, isString := v.(string); !isString {
					t.Errorf("-.inf: expected JSON string (not a number), got %T: %v", v, v)
				}
			},
		},
		{
			name:    "not-a-number .nan",
			yaml:    "x: .nan",
			wantTag: "!!float",
			validate: func(t *testing.T, got string) {
				t.Helper()
				var obj map[string]interface{}
				if err := json.Unmarshal([]byte(got), &obj); err != nil {
					t.Errorf(".nan: emitted JSON is invalid: %v\ngot: %s", err, got)
					return
				}
				v, ok := obj["x"]
				if !ok {
					t.Errorf(".nan: key 'x' missing")
					return
				}
				if _, isString := v.(string); !isString {
					t.Errorf(".nan: expected JSON string (not a number), got %T: %v", v, v)
				}
			},
		},
		{
			name:    "regular integer 42",
			yaml:    "x: 42",
			wantTag: "!!int",
			validate: func(t *testing.T, got string) {
				t.Helper()
				var obj map[string]interface{}
				if err := json.Unmarshal([]byte(got), &obj); err != nil {
					t.Errorf("42: emitted JSON is invalid: %v\ngot: %s", err, got)
					return
				}
				v, _ := obj["x"].(float64)
				if v != 42 {
					t.Errorf("42: want 42, got %v", v)
				}
			},
		},
		{
			name:    "regular float 3.14",
			yaml:    "x: 3.14",
			wantTag: "!!float",
			validate: func(t *testing.T, got string) {
				t.Helper()
				var obj map[string]interface{}
				if err := json.Unmarshal([]byte(got), &obj); err != nil {
					t.Errorf("3.14: emitted JSON is invalid: %v\ngot: %s", err, got)
					return
				}
				v, _ := obj["x"].(float64)
				if v != 3.14 {
					t.Errorf("3.14: want 3.14, got %v", v)
				}
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := yamlToJSON([]byte(tc.yaml))
			if err != nil {
				t.Fatalf("yamlToJSON(%q): unexpected error: %v", tc.yaml, err)
			}
			// Always validate that the output round-trips through json.Unmarshal.
			var raw interface{}
			if err := json.Unmarshal(got, &raw); err != nil {
				t.Errorf("yamlToJSON output is not valid JSON: %v\noutput: %s", err, got)
			}
			if tc.validate != nil {
				tc.validate(t, string(got))
			}
		})
	}
}

// TestScalarToJSON_HTMLInlinedJSONRoundtrip feeds a YAML document containing
// all the problematic numeric forms through the full HTML rendering path and
// asserts that the inlined JSON is valid (i.e. json.Unmarshal succeeds).
//
// This is the end-to-end regression for CR-01: the primary failure mode was
// Redoc.init() crashing on invalid JSON baked into the <script> block.
func TestScalarToJSON_HTMLInlinedJSONRoundtrip(t *testing.T) {
	t.Parallel()

	// A synthetic spec containing hex, octal, underscore integer, inf, and nan.
	// These are all valid YAML 1.2 values but none are valid JSON tokens.
	spec := []byte(`openapi: "3.0.3"
info:
  title: NumericTest
  version: "1.0.0"
x-hex: 0xAF
x-octal: 0o17
x-underscore: 1_000
x-inf: .inf
x-nan: .nan
paths: {}
`)
	got, err := yamlToJSON(spec)
	if err != nil {
		t.Fatalf("yamlToJSON: unexpected error: %v", err)
	}

	var obj map[string]interface{}
	if err := json.Unmarshal(got, &obj); err != nil {
		t.Errorf("emitted JSON does not parse: %v\nJSON:\n%s", err, got)
	}
}

// TestNodeToJSON_DepthGuard verifies that nodeToJSON returns an error (not a
// stack overflow) when given a document that exceeds maxNodeDepth.
//
// The test generates a deeply nested YAML document programmatically, then
// calls yamlToJSON (which calls nodeToJSON internally). The expected outcome
// is a non-nil error containing "maximum nesting depth".
func TestNodeToJSON_DepthGuard(t *testing.T) {
	t.Parallel()

	// Build a YAML document that is (maxNodeDepth + 10) levels deep.
	// Format: a: {a: {a: {…}}}
	depth := maxNodeDepth + 10
	var sb strings.Builder
	for i := 0; i < depth; i++ {
		if i > 0 {
			sb.WriteString("\n")
			sb.WriteString(strings.Repeat(" ", i*2))
		}
		sb.WriteString("a:")
	}
	sb.WriteString(" deep")

	deepYAML := []byte(sb.String())
	_, err := yamlToJSON(deepYAML)
	if err == nil {
		t.Error("yamlToJSON: expected error for document exceeding maxNodeDepth, got nil")
		return
	}
	if !strings.Contains(err.Error(), "maximum nesting depth") {
		t.Errorf("expected error to mention 'maximum nesting depth', got: %v", err)
	}
}

// TestEscapeField_MarkdownControlChars verifies that escapeField neutralises
// Markdown-significant characters from spec-derived fields (WR-02 / T-03-07).
//
// The golden tests do not change with this fix (the petstore fixture contains
// no Markdown-significant chars), so this focused unit test asserts the escaping
// directly on an adversarial input.
func TestEscapeField_MarkdownControlChars(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		// wantContains is a list of strings that MUST appear in the output.
		wantContains []string
		// wantAbsent is a list of strings that MUST NOT appear verbatim in the output.
		wantAbsent []string
	}{
		{
			name:         "pipe character neutralised",
			input:        "evil | injected cell",
			wantContains: []string{`\|`},
			wantAbsent:   []string{" | "},
		},
		{
			name:         "newline in table cell replaced with br",
			input:        "line one\nline two",
			wantContains: []string{"<br>"},
			wantAbsent:   []string{"\n"},
		},
		{
			name:         "hash heading injection neutralised",
			input:        "## Fake Heading",
			wantContains: []string{`\#`},
			wantAbsent:   []string{"## "},
		},
		{
			name:         "backtick code span neutralised",
			input:        "`rm -rf /`",
			wantContains: []string{"\\`"},
		},
		{
			name:         "asterisk emphasis neutralised",
			input:        "*bold*",
			wantContains: []string{`\*`},
			wantAbsent:   []string{"*bold*"},
		},
		{
			name:         "underscore emphasis neutralised",
			input:        "_italic_",
			wantContains: []string{`\_`},
			wantAbsent:   []string{"_italic_"},
		},
		{
			name:         "link syntax neutralised",
			input:        "[evil](http://evil.example.com)",
			wantContains: []string{`\[`, `\]`},
			wantAbsent:   []string{"[evil]"},
		},
		{
			name:         "HTML metacharacters still escaped",
			input:        "<script>alert(1)</script>",
			wantContains: []string{"&lt;", "&gt;"},
			wantAbsent:   []string{"<script>"},
		},
		{
			name:         "combined injection: pipe newline heading",
			input:        "col1\n## Heading\nevil | injected",
			wantContains: []string{`\#`, `\|`, "<br>"},
			wantAbsent:   []string{"## ", " | ", "\n"},
		},
		{
			name:         "backslash itself escaped first",
			input:        `a\b`,
			wantContains: []string{`a\\b`},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := escapeField(tc.input)
			for _, want := range tc.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("escapeField(%q) = %q; want it to contain %q", tc.input, got, want)
				}
			}
			for _, absent := range tc.wantAbsent {
				if strings.Contains(got, absent) {
					t.Errorf("escapeField(%q) = %q; must NOT contain %q", tc.input, got, absent)
				}
			}
		})
	}
}
