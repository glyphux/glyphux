package contract

import (
	"strings"
	"testing"
)

func validDoc() string {
	return `{
		"contract_version": "content-composition/v0",
		"site": {"name": "Test Site"},
		"content_types": {
			"article": {
				"fields": {
					"title":  {"type": "string", "required": true, "localized": true},
					"body":   {"type": "richtext"},
					"author": {"type": "relation", "to": "person"}
				}
			},
			"person": {
				"fields": {
					"name": {"type": "string", "required": true}
				}
			}
		}
	}`
}

func TestParseValidComposition(t *testing.T) {
	comp, err := Parse([]byte(validDoc()))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if comp.Site.Name != "Test Site" {
		t.Errorf("site name = %q", comp.Site.Name)
	}
	if len(comp.ContentTypes) != 2 {
		t.Errorf("content types = %d, want 2", len(comp.ContentTypes))
	}
	if f := comp.ContentTypes["article"].Fields["author"]; f.To != "person" {
		t.Errorf("relation target = %q, want person", f.To)
	}
}

func TestParseRejectsInvalid(t *testing.T) {
	cases := map[string]struct {
		doc     string
		wantErr string
	}{
		"unknown top-level field": {
			doc:     `{"contract_version": "content-composition/v0", "site": {"name": "x"}, "bogus": 1}`,
			wantErr: "unknown field",
		},
		"wrong contract version": {
			doc:     `{"contract_version": "content-composition/v9", "site": {"name": "x"}}`,
			wantErr: "unsupported version",
		},
		"missing site name": {
			doc:     `{"contract_version": "content-composition/v0", "site": {"name": ""}}`,
			wantErr: "site.name",
		},
		"unknown field type": {
			doc: `{"contract_version": "content-composition/v0", "site": {"name": "x"},
				"content_types": {"a": {"fields": {"f": {"type": "blob"}}}}}`,
			wantErr: `unknown field type "blob"`,
		},
		"relation without target": {
			doc: `{"contract_version": "content-composition/v0", "site": {"name": "x"},
				"content_types": {"a": {"fields": {"f": {"type": "relation"}}}}}`,
			wantErr: "must name a target",
		},
		"relation to undeclared type": {
			doc: `{"contract_version": "content-composition/v0", "site": {"name": "x"},
				"content_types": {"a": {"fields": {"f": {"type": "relation", "to": "ghost"}}}}}`,
			wantErr: `"ghost" is not a declared content type`,
		},
		"target on non-relation": {
			doc: `{"contract_version": "content-composition/v0", "site": {"name": "x"},
				"content_types": {"a": {"fields": {"f": {"type": "string", "to": "a"}}}}}`,
			wantErr: "only relation fields",
		},
		"empty content type": {
			doc: `{"contract_version": "content-composition/v0", "site": {"name": "x"},
				"content_types": {"a": {"fields": {}}}}`,
			wantErr: "at least one field",
		},
		"invalid type name": {
			doc: `{"contract_version": "content-composition/v0", "site": {"name": "x"},
				"content_types": {"Bad-Name": {"fields": {"f": {"type": "string"}}}}}`,
			wantErr: "lowercase identifier",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Parse([]byte(tc.doc))
			if err == nil {
				t.Fatal("Parse accepted an invalid composition")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not mention %q", err, tc.wantErr)
			}
		})
	}
}

func TestValidateReportsAllViolations(t *testing.T) {
	c := &Composition{
		ContractVersion: "nope",
		ContentTypes: map[string]ContentType{
			"a": {Fields: map[string]Field{"f": {Type: "blob"}}},
		},
	}
	err := c.Validate()
	verrs, ok := err.(ValidationErrors)
	if !ok {
		t.Fatalf("want ValidationErrors, got %T (%v)", err, err)
	}
	if len(verrs) != 3 { // version, site.name, field type
		t.Errorf("got %d violations, want 3: %v", len(verrs), verrs)
	}
}

func TestEncodeRoundTrip(t *testing.T) {
	comp, err := Parse([]byte(validDoc()))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := comp.Encode()
	if err != nil {
		t.Fatal(err)
	}
	again, err := Parse(raw)
	if err != nil {
		t.Fatalf("re-Parse of Encode output: %v", err)
	}
	if again.Site.Name != comp.Site.Name || len(again.ContentTypes) != len(comp.ContentTypes) {
		t.Error("round trip lost data")
	}
}
