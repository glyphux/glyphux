package content

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/glyphux/glyphux/pkg/contract"
)

// ErrUnknownType reports a content type not declared in the composition.
var ErrUnknownType = errors.New("unknown content type")

// ErrValidation reports that item data violates its content type. The concrete
// error is a *ValidationError carrying every field-level problem.
var ErrValidation = errors.New("content validation failed")

// ValidationError aggregates every field-level violation for one write.
type ValidationError struct {
	Type   string
	Issues []string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s (%q)", ErrValidation.Error(), strings.Join(e.Issues, "; "), e.Type)
}

func (e *ValidationError) Is(target error) bool { return target == ErrValidation }

// validate checks item data against a declared content type, reporting every
// violation at once: unknown fields, missing required fields, and values whose
// JSON kind does not match the declared field type.
func validate(typeName string, ct contract.ContentType, data map[string]any) error {
	var issues []string

	// Unknown fields — the composition is the schema; extras are rejected.
	for name := range data {
		if _, ok := ct.Fields[name]; !ok {
			issues = append(issues, fmt.Sprintf("unknown field %q", name))
		}
	}

	// Declared fields — required presence and value-kind checks.
	for name, f := range ct.Fields {
		v, present := data[name]
		if !present || v == nil {
			if f.Required {
				issues = append(issues, fmt.Sprintf("field %q is required", name))
			}
			continue
		}
		if msg := checkKind(name, f.Type, v); msg != "" {
			issues = append(issues, msg)
		}
	}

	if len(issues) == 0 {
		return nil
	}
	sort.Strings(issues)
	return &ValidationError{Type: typeName, Issues: issues}
}

// checkKind returns a violation message if v's JSON kind is incompatible with
// the declared field type, or "" if it is acceptable. Values arrive as decoded
// JSON, so numbers are float64, booleans bool, strings string.
func checkKind(name string, ft contract.FieldType, v any) string {
	ok := true
	switch ft {
	case contract.FieldString, contract.FieldRichText, contract.FieldDate,
		contract.FieldRelation, contract.FieldMedia:
		_, ok = v.(string)
	case contract.FieldNumber:
		_, ok = v.(float64)
	case contract.FieldBoolean:
		_, ok = v.(bool)
	}
	if !ok {
		return fmt.Sprintf("field %q must be %s", name, ft)
	}
	return ""
}
