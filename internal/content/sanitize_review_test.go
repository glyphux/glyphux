// CORE-04 REVIEW (behavior-first): rich-text sanitization at the write
// boundary.
//
// Given HTML input carrying <script>, <img onerror=...>, <iframe>, and
// javascript: URLs, When sanitizeRichText runs (the actual function
// internal/content applies to every richtext field value), Then the output
// contains no script tags, no on* attributes, no iframes, no javascript:
// URLs — and benign content (paragraphs, bold) is preserved.
package content

import (
	"strings"
	"testing"

	"github.com/glyphux/glyphux/pkg/contract"
)

// TestReviewSanitizeStripsActiveContent — non-localized richtext field.
func TestReviewSanitizeStripsActiveContent(t *testing.T) {
	t.Run("Given <script>, <img onerror>, <iframe>, javascript: URLs in a richtext field, When sanitizeRichText runs, Then active content is stripped and benign HTML preserved", func(t *testing.T) {
		ct := contract.ContentType{Fields: map[string]contract.Field{
			"body": {Type: contract.FieldRichText},
		}}
		input := `<p>Hello <strong>world</strong></p>` +
			`<script>alert(1)</script>` +
			`<img src="x" onerror="alert(1)">` +
			`<iframe src="https://evil.example"></iframe>` +
			`<a href="javascript:alert(1)">click me</a>`
		t.Logf("Given input: %s", input)
		t.Logf("When  sanitizeRichText(ct, data) runs at the write boundary")
		data := map[string]any{"body": input}
		sanitizeRichText(ct, data)
		out, _ := data["body"].(string)

		check(t, "output contains no <script> tag", !strings.Contains(out, "<script"), out)
		check(t, "output contains no on* handler attributes (onerror)", !strings.Contains(out, "onerror"), out)
		check(t, "output contains no <iframe>", !strings.Contains(out, "iframe"), out)
		check(t, "output contains no javascript: URL", !strings.Contains(out, "javascript:"), out)
		check(t, "benign <strong>world</strong> is preserved", strings.Contains(out, "<strong>world</strong>"), out)
		check(t, "benign <p>Hello is preserved", strings.Contains(out, "<p>Hello"), out)
	})
}

// TestReviewSanitizeLocalizedField — the localized rich-text path.
func TestReviewSanitizeLocalizedField(t *testing.T) {
	t.Run("Given a Localized richtext field with per-locale hostile HTML, When sanitizeRichText runs, Then every locale is sanitized", func(t *testing.T) {
		ct := contract.ContentType{Fields: map[string]contract.Field{
			"body": {Type: contract.FieldRichText, Localized: true},
		}}
		data := map[string]any{
			"body": map[string]any{
				"en": `<p>Fine</p><script>alert(1)</script>`,
				"fr": `<img src=x onerror=alert(2)>`,
			},
		}
		t.Logf("When  sanitizeRichText runs over a localized field")
		sanitizeRichText(ct, data)
		locales, _ := data["body"].(map[string]any)
		en, _ := locales["en"].(string)
		fr, _ := locales["fr"].(string)
		check(t, "en locale strips <script>", !strings.Contains(en, "<script"), en)
		check(t, "en locale keeps <p>Fine</p>", strings.Contains(en, "<p>Fine</p>"), en)
		check(t, "fr locale strips onerror", !strings.Contains(fr, "onerror"), fr)
	})
}

// TestReviewSanitizeLeavesOtherFieldsAlone — non-richtext fields untouched.
func TestReviewSanitizeLeavesOtherFieldsAlone(t *testing.T) {
	t.Run("Given a string field alongside a richtext field, When sanitizeRichText runs, Then only the richtext field is sanitized", func(t *testing.T) {
		ct := contract.ContentType{Fields: map[string]contract.Field{
			"title": {Type: contract.FieldString},
			"body":  {Type: contract.FieldRichText},
		}}
		data := map[string]any{
			"title": `<script>alert(1)</script>`,
			"body":  `<script>alert(1)</script>`,
		}
		t.Logf("When  sanitizeRichText runs")
		sanitizeRichText(ct, data)
		check(t, "string field 'title' is NOT sanitized (sanitizer only owns richtext)", data["title"] == `<script>alert(1)</script>`, data["title"])
		check(t, "richtext field 'body' IS sanitized", !strings.Contains(data["body"].(string), "<script"), data["body"])
	})
}

func check(t *testing.T, what string, pass bool, out any) {
	t.Helper()
	if pass {
		t.Logf("PASS: %s — output: %q", what, out)
	} else {
		t.Errorf("FAIL: %s — output: %q", what, out)
	}
}
