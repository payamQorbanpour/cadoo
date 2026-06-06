// Package apidocs (parse.go) implements OpenAPI/Swagger spec parsing for the
// API-docs generator. loadSpec validates, version-detects, and extracts an
// ordered path model from the spec bytes. SSRF and OOM mitigations are applied
// before any parsing occurs.
package apidocs

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/pb33f/libopenapi"
	libopenapiv "github.com/pb33f/libopenapi-validator"
	"github.com/pb33f/libopenapi/datamodel"
	v2high "github.com/pb33f/libopenapi/datamodel/high/v2"
	v3high "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/utils"
)

// maxSpecSize is the maximum raw byte length accepted before parsing (T-03-04 OOM guard).
// Specs over this size return an error so the generator skips — no parsing occurs.
const maxSpecSize = 5 * 1024 * 1024 // 5 MiB

// paramItem holds a single parameter extracted from a path operation.
// Consumed by renderMarkdown (Plan 05).
type paramItem struct {
	// Name is the parameter name.
	Name string
	// In is the parameter location ("query", "path", "header", "body").
	In string
}

// operationItem holds one HTTP operation extracted from a spec path.
// Consumed by renderMarkdown (Plan 05).
type operationItem struct {
	// Method is the HTTP verb in uppercase (GET, POST, PUT, DELETE, …).
	Method string
	// Path is the raw path string (e.g. "/pets/{id}").
	Path string
	// Summary is the operation's short summary.
	Summary string
	// Description is the operation's full description.
	Description string
	// Parameters is the ordered list of parameters in spec source order.
	Parameters []paramItem
}

// specModel holds the parsed, version-detected, validated spec data.
// Returned by loadSpec and consumed by the render functions (Plans 04–05).
type specModel struct {
	// Version is the raw spec version string (e.g. "3.0.3", "2.0").
	Version string
	// Title is the API title from info.title.
	Title string
	// Paths is the sorted, ordered list of operations extracted from the spec.
	// Populated for both OAS 3.x and Swagger 2.0; ordering is alphabetical by
	// path key for golden-file determinism.
	Paths []operationItem
}

// isSupportedOAS3Version reports whether version is within the supported OAS 3.x
// range (3.0.x and 3.1.x). OAS 3.2+ is explicitly rejected because our tooling
// has not been validated against future major minor versions (D-03, D-09).
func isSupportedOAS3Version(version string) bool {
	return strings.HasPrefix(version, "3.0.") || strings.HasPrefix(version, "3.1.")
}

// loadSpec parses specBytes, detects the OpenAPI/Swagger version, validates
// (OAS 3.x only), and extracts an ordered path model.
//
// Security guards applied in order:
//  1. OOM guard: rejects specs larger than maxSpecSize before any parsing.
//  2. SSRF guard: uses NewDocumentWithConfiguration with AllowRemoteReferences
//     and AllowFileReferences both set to false (T-03-03).
//
// For OAS 3.x, the document is also validated via libopenapi-validator (T-03-06).
// For Swagger 2.0, parse success is treated as "valid enough" because no
// maintained Go Swagger 2.0 schema validator exists (T-03-05).
//
// Returns a non-nil error (with a reason) on every failure so GenerateMulti
// can skip with a log (D-10). Never returns a partial result on error.
func loadSpec(specBytes []byte) (*specModel, error) {
	// 1. OOM guard — must run BEFORE any parsing (T-03-04).
	if len(specBytes) > maxSpecSize {
		return nil, fmt.Errorf("apidocs: spec too large (%d bytes, limit %d)", len(specBytes), maxSpecSize)
	}

	// 2. SSRF guard — disable remote and file $ref resolution (T-03-03).
	cfg := &datamodel.DocumentConfiguration{
		AllowRemoteReferences: false,
		AllowFileReferences:   false,
	}
	doc, err := libopenapi.NewDocumentWithConfiguration(specBytes, cfg)
	if err != nil {
		return nil, fmt.Errorf("apidocs: parse spec: %w", err)
	}

	info := doc.GetSpecInfo()
	switch info.SpecType {
	case utils.OpenApi3:
		// D-03, D-09: supported set = OAS 3.0.x and 3.1.x only.
		// OAS 3.2+ is not yet supported; libopenapi still parses it as OpenApi3
		// but we must reject it explicitly so the generator skips rather than
		// publishing docs for a spec version we have not validated our tooling against.
		if !isSupportedOAS3Version(info.Version) {
			return nil, fmt.Errorf("apidocs: unsupported spec version %q (want swagger 2.0, openapi 3.x)", info.Version)
		}
		return loadV3(doc, info.Version)
	case utils.OpenApi2:
		return parseSwagger2(doc, info.Version)
	default:
		return nil, fmt.Errorf("apidocs: unsupported spec version %q (want swagger 2.0, openapi 3.x)", info.Version)
	}
}

// loadV3 builds and validates an OAS 3.x model, then extracts path operations.
// Returns an error on build failure or schema validation failure (D-10).
func loadV3(doc libopenapi.Document, version string) (*specModel, error) {
	v3Model, err := doc.BuildV3Model()
	if err != nil {
		return nil, fmt.Errorf("apidocs: build v3 model: %w", err)
	}

	// Validate the document against the OpenAPI 3.x schema (T-03-06).
	// NewValidator also calls BuildV3Model internally; errors from that are
	// reported as initialization errors (not validation errors).
	docValidator, initErrs := libopenapiv.NewValidator(doc)
	if len(initErrs) > 0 {
		return nil, fmt.Errorf("apidocs: validator init: %v", initErrs[0])
	}
	valid, valErrs := docValidator.ValidateDocument()
	if !valid && len(valErrs) > 0 {
		return nil, fmt.Errorf("apidocs: spec validation failed: %s", valErrs[0].Message)
	}

	sm := &specModel{Version: version}
	if v3Model.Model.Info != nil {
		sm.Title = v3Model.Model.Info.Title
	}

	sm.Paths = extractV3Paths(v3Model.Model)
	return sm, nil
}

// extractV3Paths collects operations from an OAS 3.x Document, sorted by path
// key for golden-file determinism (RESEARCH.md Pattern 3).
func extractV3Paths(doc v3high.Document) []operationItem {
	if doc.Paths == nil || doc.Paths.PathItems == nil {
		return nil
	}

	// Collect keys first, then sort for determinism.
	var keys []string
	for k := range doc.Paths.PathItems.KeysFromOldest() {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var ops []operationItem
	for _, pathKey := range keys {
		item, ok := doc.Paths.PathItems.Get(pathKey)
		if !ok || item == nil {
			continue
		}
		ops = append(ops, v3PathItemToOps(pathKey, item)...)
	}
	return ops
}

// v3PathItemToOps extracts individual operations from a V3 PathItem in
// canonical HTTP verb order (GET, PUT, POST, DELETE).
func v3PathItemToOps(pathKey string, item *v3high.PathItem) []operationItem {
	type namedOp struct {
		method string
		op     *v3high.Operation
	}
	// Canonical HTTP verb order (deterministic within a single path entry).
	candidates := []namedOp{
		{"GET", item.Get},
		{"PUT", item.Put},
		{"POST", item.Post},
		{"DELETE", item.Delete},
	}

	var ops []operationItem
	for _, c := range candidates {
		if c.op == nil {
			continue
		}
		oi := operationItem{
			Method:      c.method,
			Path:        pathKey,
			Summary:     c.op.Summary,
			Description: c.op.Description,
		}
		for _, p := range c.op.Parameters {
			if p == nil {
				continue
			}
			oi.Parameters = append(oi.Parameters, paramItem{Name: p.Name, In: p.In})
		}
		ops = append(ops, oi)
	}
	return ops
}

// parseSwagger2 handles Swagger 2.0 specs using the deprecated libopenapi v2
// model. All v2 API calls are isolated in this function so they can be removed
// as a unit when libopenapi drops v2 model support.
//
// TODO: libopenapi v2 model is deprecated and will be removed; revisit when
// libopenapi drops it. See: https://pb33f.io/libopenapi/swagger/
func parseSwagger2(doc libopenapi.Document, version string) (*specModel, error) {
	slog.Warn("apidocs: Swagger 2.0 spec; v2 model deprecated; limited validation",
		"version", version)

	// Build the v2 model — parse success is treated as "valid enough" because
	// no maintained Go Swagger 2.0 schema validator exists (T-03-05 accept/isolated).
	v2Model, err := doc.BuildV2Model()
	if err != nil {
		return nil, fmt.Errorf("apidocs: build v2 model: %w", err)
	}

	sm := &specModel{Version: version}
	if v2Model.Model.Info != nil {
		sm.Title = v2Model.Model.Info.Title
	}

	sm.Paths = extractV2Paths(v2Model.Model)
	return sm, nil
}

// extractV2Paths collects operations from a Swagger 2.0 Swagger model, sorted
// by path key for determinism.
func extractV2Paths(doc v2high.Swagger) []operationItem {
	if doc.Paths == nil || doc.Paths.PathItems == nil {
		return nil
	}

	var keys []string
	for k := range doc.Paths.PathItems.KeysFromOldest() {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var ops []operationItem
	for _, pathKey := range keys {
		item, ok := doc.Paths.PathItems.Get(pathKey)
		if !ok || item == nil {
			continue
		}
		ops = append(ops, v2PathItemToOps(pathKey, item)...)
	}
	return ops
}

// v2PathItemToOps extracts individual operations from a Swagger 2.0 PathItem.
func v2PathItemToOps(pathKey string, item *v2high.PathItem) []operationItem {
	type namedOp struct {
		method string
		op     *v2high.Operation
	}
	candidates := []namedOp{
		{"GET", item.Get},
		{"PUT", item.Put},
		{"POST", item.Post},
		{"DELETE", item.Delete},
	}

	var ops []operationItem
	for _, c := range candidates {
		if c.op == nil {
			continue
		}
		oi := operationItem{
			Method:      c.method,
			Path:        pathKey,
			Summary:     c.op.Summary,
			Description: c.op.Description,
		}
		for _, p := range c.op.Parameters {
			if p == nil {
				continue
			}
			oi.Parameters = append(oi.Parameters, paramItem{Name: p.Name, In: p.In})
		}
		ops = append(ops, oi)
	}
	return ops
}
