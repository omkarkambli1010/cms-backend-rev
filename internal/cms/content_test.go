package cms

import (
	"reflect"
	"testing"
)

func TestBuildContentDataValidatesAndDropsEmpties(t *testing.T) {
	fields := []FieldDef{
		{Name: "title", Kind: "text", Required: true, Max: 10},
		{Name: "link", Kind: "href"},
		{Name: "groups", Kind: "multiselect", Options: []string{"board", "managing"}},
		{Name: "bio", Kind: "lines"},
		{Name: "photo_media", Kind: "media"},
		{Name: "count", Kind: "number"},
	}

	data, err := BuildContentData(fields, map[string]any{
		"title":       "  Hello ",
		"link":        "",
		"groups":      []any{"board", "board"},
		"bio":         []any{},
		"photo_media": float64(12),
		"count":       "7",
		"smuggled":    "not in the registry",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := map[string]any{
		"title":       "Hello",
		"groups":      []string{"board"},
		"photo_media": int64(12),
		"count":       int64(7),
	}
	if !reflect.DeepEqual(data, want) {
		t.Fatalf("got %#v, want %#v", data, want)
	}
}

func TestBuildContentDataRejectsBadValues(t *testing.T) {
	fields := []FieldDef{
		{Name: "title", Kind: "text", Required: true, Max: 5},
		{Name: "link", Kind: "href"},
		{Name: "groups", Kind: "multiselect", Options: []string{"board"}},
	}

	if _, err := BuildContentData(fields, map[string]any{"title": "", "link": "javascript:alert(1)", "groups": []any{"nope"}}); err == nil {
		t.Fatal("expected a validation error")
	}
	if _, err := BuildContentData(fields, map[string]any{"title": "far too long"}); err == nil {
		t.Fatal("expected a length error")
	}
}

func TestItemKeyAndFlatten(t *testing.T) {
	col := &ContentCollection{Key: "leaders", KeyField: "slug"}

	if got := ItemKey(col, map[string]any{"slug": "a-b"}); got != "a-b" {
		t.Fatalf("ItemKey = %v", got)
	}
	if got := ItemKey(col, map[string]any{}); got != nil {
		t.Fatalf("ItemKey of a keyless row = %v, want nil", got)
	}

	flat := FlattenItem(map[string]any{
		"id": "3", "item_key": "a-b", "is_active": true,
		"data": map[string]any{"name": "N"},
	}, col)
	if flat["slug"] != "a-b" || flat["name"] != "N" || flat["id"] != "3" {
		t.Fatalf("flatten = %#v", flat)
	}
}
