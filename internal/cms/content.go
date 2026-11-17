package cms

import (
	"fmt"

	"cms-backend/internal/apierr"
)

// Content pages. The home folds are described by SectionDef/CollectionDef and
// stored one family of routines each. The inner pages are stored generically
// (cms.content_blocks / cms.content_items), so what a section holds is only
// ever a list of fields here, and the routines are the same for all of them.
//
// registry.json's "pages" member is generated from cms-website's pages.ts by
// scripts/sync-registry.mjs; neither file is edited by hand.

// ContentCollection is one list inside a content section.
type ContentCollection struct {
	Key   string     `json:"key"`
	Label string     `json:"label"`
	Noun  string     `json:"noun"`
	// KeyField names the field whose value is also kept as the row's item_key.
	KeyField string     `json:"keyField,omitempty"`
	Fields   []FieldDef `json:"fields"`
}

// ContentSection is a section stored generically: a block of its own fields
// and any number of collections.
type ContentSection struct {
	Key         string              `json:"key"`
	Label       string              `json:"label"`
	Block       []FieldDef          `json:"block"`
	Collections []ContentCollection `json:"collections"`
}

// PageDef is one content page. SectionOrder lists every section on the page;
// Sections holds only the generically stored ones (a reused home fold such as
// the FAQ is described by the home registry instead).
type PageDef struct {
	Slug         string                     `json:"slug"`
	Label        string                     `json:"label"`
	Route        string                     `json:"route"`
	SectionOrder []string                   `json:"sectionOrder"`
	Sections     map[string]*ContentSection `json:"sections"`
}

func ResolvePage(slug string) *PageDef { return reg.Pages[slug] }

// ResolveContentSection returns the generic definition of a page's section, or
// nil when the section is a reused home fold (or does not exist).
func ResolveContentSection(page, section string) *ContentSection {
	p := reg.Pages[page]
	if p == nil {
		return nil
	}
	return p.Sections[section]
}

func (s *ContentSection) Collection(key string) *ContentCollection {
	for i := range s.Collections {
		if s.Collections[i].Key == key {
			return &s.Collections[i]
		}
	}
	return nil
}

// PublicCollection is the shape sent to the dashboard.
func (c *ContentCollection) Public() map[string]any {
	return map[string]any{"key": c.Key, "label": c.Label, "noun": c.Noun, "parent": "section", "fields": c.Fields}
}

// BuildContentData validates body against fields and returns the object stored
// in `data`. Every field is checked with the same rules the home folds use, and
// anything the registry does not name is dropped: a request cannot add a key no
// screen could have produced.
//
// Empty optional values are omitted rather than stored as null, so the public
// payload only carries what an editor actually filled in — which is what lets
// the site fall back to its built-in copy field by field.
func BuildContentData(fields []FieldDef, body map[string]any) (map[string]any, error) {
	problems := map[string]string{}
	data := make(map[string]any, len(fields))

	for _, f := range fields {
		c := coerce(f, body[f.Name])
		if c.problem != "" {
			problems[f.Name] = c.problem
			continue
		}
		if c.value == nil {
			continue
		}
		switch v := c.value.(type) {
		case string:
			if v == "" {
				continue
			}
		case []string:
			if len(v) == 0 {
				continue
			}
		}
		data[f.Name] = c.value
	}

	if len(problems) > 0 {
		return nil, apierr.Validation(problems, "")
	}
	return data, nil
}

// ItemKey is the stable handle stored beside a row's data.
func ItemKey(c *ContentCollection, data map[string]any) any {
	if c.KeyField == "" {
		return nil
	}
	if s, ok := data[c.KeyField].(string); ok && s != "" {
		return s
	}
	return nil
}

// FlattenItem turns a content_items row into the flat shape the dashboard's
// collection screens read: the data's fields at the top level, beside id,
// is_active and display_order.
func FlattenItem(row map[string]any, c *ContentCollection) map[string]any {
	out := map[string]any{}
	if d, ok := row["data"].(map[string]any); ok {
		for k, v := range d {
			out[k] = v
		}
	}
	// The key field is stored once, as item_key; surface it under its own name
	// so the form shows it and the list can search it.
	if c != nil && c.KeyField != "" {
		if _, present := out[c.KeyField]; !present {
			if k, ok := row["item_key"].(string); ok && k != "" {
				out[c.KeyField] = k
			}
		}
	}
	for _, k := range []string{"id", "section_id", "item_key", "display_order", "is_active", "created_at", "updated_at"} {
		if v, ok := row[k]; ok {
			out[k] = v
		}
	}
	return out
}

func (c *ContentCollection) String() string { return fmt.Sprintf("content collection %s", c.Key) }
