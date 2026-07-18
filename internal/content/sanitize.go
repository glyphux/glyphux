package content

import (
	"github.com/microcosm-cc/bluemonday"

	"github.com/glyphux/glyphux/pkg/contract"
)

// richTextPolicy is the one HTML sanitization policy every rich-text field
// value is passed through at the write boundary (slice 1.9). It allows the
// common safe formatting elements a rich-text editor produces (paragraphs,
// headings, lists, emphasis, links, images) and strips everything else —
// <script>, inline event handlers (onerror, onclick, ...), javascript: URLs,
// <iframe>, <style>, and so on — closing the stored-XSS gap where a theme or
// client later renders a content field's value as raw HTML.
var richTextPolicy = bluemonday.UGCPolicy()

// sanitizeRichText runs every rich-text field's value in data through
// richTextPolicy, in place. It is called once, at the write boundary
// (Create/Update/Rollback), so every stored value — regardless of which
// caller wrote it — is already safe by the time anything reads it back;
// callers never need to remember to sanitize on read.
func sanitizeRichText(ct contract.ContentType, data map[string]any) {
	for name, f := range ct.Fields {
		if f.Type != contract.FieldRichText {
			continue
		}
		v, ok := data[name]
		if !ok || v == nil {
			continue
		}
		if f.Localized {
			locales, ok := v.(map[string]any)
			if !ok {
				continue // wrong kind; validate already rejects this
			}
			for locale, lv := range locales {
				if s, ok := lv.(string); ok {
					locales[locale] = richTextPolicy.Sanitize(s)
				}
			}
			continue
		}
		if s, ok := v.(string); ok {
			data[name] = richTextPolicy.Sanitize(s)
		}
	}
}
