// Package contract defines the public composition contract types.
//
// The composition contract is the architectural center of Glyphux (Principle 1):
// a typed, versioned, serializable document describing what composes with what.
// Every surface — API, CLI, SDK, themes, plugins, the builder — is a client of
// these types; none owns them (Principle 2).
//
// This package is public from day one and importable by plugin and theme
// authors. It must never import kernel internals.
package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Version identifies a composition contract layer version, e.g.
// "content-composition/v0". Each layer is independently versioned (§3.2).
type Version string

// ContentCompositionV0 is the Phase-0 contract version for Layer 1
// (Content Composition).
const ContentCompositionV0 Version = "content-composition/v0"

// FieldType enumerates the field kinds Layer 1 understands.
type FieldType string

const (
	FieldString   FieldType = "string"
	FieldRichText FieldType = "richtext"
	FieldNumber   FieldType = "number"
	FieldBoolean  FieldType = "boolean"
	FieldDate     FieldType = "date"
	FieldRelation FieldType = "relation"
	FieldMedia    FieldType = "media"
)

var fieldTypes = map[FieldType]bool{
	FieldString: true, FieldRichText: true, FieldNumber: true,
	FieldBoolean: true, FieldDate: true, FieldRelation: true, FieldMedia: true,
}

// Composition is the root composition document — the single source of truth
// for what the running product is composed of.
type Composition struct {
	ContractVersion Version                `json:"contract_version"`
	Site            Site                   `json:"site"`
	ContentTypes    map[string]ContentType `json:"content_types,omitempty"`
	Capabilities    []string               `json:"capabilities,omitempty"`
}

// Site holds site-level configuration written by the first-run wizard.
type Site struct {
	Name string `json:"name"`
}

// ContentType declares a typed content shape.
type ContentType struct {
	Fields map[string]Field `json:"fields"`
}

// Field declares one typed field on a content type.
type Field struct {
	Type      FieldType `json:"type"`
	Required  bool      `json:"required,omitempty"`
	Localized bool      `json:"localized,omitempty"`
	// To names the target content type for relation fields.
	To string `json:"to,omitempty"`
}

// ValidationError describes one contract violation at a JSON-ish path.
type ValidationError struct {
	Path    string
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Path, e.Message)
}

// ValidationErrors aggregates all violations found in one pass.
type ValidationErrors []ValidationError

func (e ValidationErrors) Error() string {
	if len(e) == 1 {
		return "composition invalid: " + e[0].Error()
	}
	return fmt.Sprintf("composition invalid: %d violations (first: %s)", len(e), e[0].Error())
}

// Parse decodes and validates a composition document. Invalid documents are
// rejected; a composition that Parse returns is guaranteed contract-valid.
func Parse(raw []byte) (*Composition, error) {
	var c Composition
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("parse composition: %w", err)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// Validate checks the composition against the contract schema. It reports
// every violation, not just the first.
func (c *Composition) Validate() error {
	var errs ValidationErrors

	if c.ContractVersion != ContentCompositionV0 {
		errs = append(errs, ValidationError{
			Path:    "contract_version",
			Message: fmt.Sprintf("unsupported version %q (supported: %s)", c.ContractVersion, ContentCompositionV0),
		})
	}
	if c.Site.Name == "" {
		errs = append(errs, ValidationError{Path: "site.name", Message: "must not be empty"})
	}

	for typeName, ct := range c.ContentTypes {
		if !validIdent(typeName) {
			errs = append(errs, ValidationError{
				Path:    "content_types." + typeName,
				Message: "content type name must be a lowercase identifier (a-z, 0-9, _)",
			})
		}
		if len(ct.Fields) == 0 {
			errs = append(errs, ValidationError{
				Path:    "content_types." + typeName + ".fields",
				Message: "must declare at least one field",
			})
		}
		for fieldName, f := range ct.Fields {
			path := "content_types." + typeName + ".fields." + fieldName
			if !validIdent(fieldName) {
				errs = append(errs, ValidationError{Path: path, Message: "field name must be a lowercase identifier (a-z, 0-9, _)"})
			}
			if !fieldTypes[f.Type] {
				errs = append(errs, ValidationError{Path: path + ".type", Message: fmt.Sprintf("unknown field type %q", f.Type)})
			}
			if f.Type == FieldRelation {
				if f.To == "" {
					errs = append(errs, ValidationError{Path: path + ".to", Message: "relation field must name a target content type"})
				} else if _, ok := c.ContentTypes[f.To]; !ok {
					errs = append(errs, ValidationError{Path: path + ".to", Message: fmt.Sprintf("relation target %q is not a declared content type", f.To)})
				}
			}
			if f.Type != FieldRelation && f.To != "" {
				errs = append(errs, ValidationError{Path: path + ".to", Message: "only relation fields may declare a target"})
			}
		}
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}

// Encode serializes the composition to its canonical on-wire JSON form.
func (c *Composition) Encode() ([]byte, error) {
	return json.MarshalIndent(c, "", "  ")
}

func validIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r == '_':
		case r >= '0' && r <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}
