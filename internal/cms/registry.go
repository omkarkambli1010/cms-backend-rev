// Package cms describes what the home page's folds contain and turns request
// bodies into the positional argument lists the stored routines take.
//
// registry.json is the single description of that content, generated from the
// dashboard's own registry so the two cannot disagree: a screen that offers a
// field the API will not save, or an API that accepts one no screen can set,
// is the failure this prevents.
//
// It is also the security boundary for the dynamic path segments. Neither
// :section nor :collection is ever interpolated into SQL: they are keys into
// this table, and a pair that is not here is a 404 before the database is
// touched. fnPrefix is data written in registry.json, so the routine name the
// db package receives is always one of a fixed set.
package cms

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

type FieldDef struct {
	Name     string   `json:"name"`
	Label    string   `json:"label"`
	Kind     string   `json:"kind"`
	Required bool     `json:"required,omitempty"`
	Options  []string `json:"options,omitempty"`
	Help     string   `json:"help,omitempty"`
	Max      int      `json:"max,omitempty"`
}

type CollectionDef struct {
	Key          string     `json:"key"`
	Label        string     `json:"label"`
	FnPrefix     string     `json:"fnPrefix"`
	Parent       string     `json:"parent"`
	Fields       []FieldDef `json:"fields"`
	CreateArgs   []string   `json:"createArgs"`
	UpdateArgs   []string   `json:"updateArgs"`
	ExtraActions []string   `json:"extraActions,omitempty"`
}

// Public is the shape sent to the dashboard: the registry's internals
// (function prefix, argument order) stay on the server.
func (c *CollectionDef) Public() map[string]any {
	return map[string]any{"key": c.Key, "label": c.Label, "parent": c.Parent, "fields": c.Fields}
}

func (c *CollectionDef) HasAction(name string) bool {
	for _, a := range c.ExtraActions {
		if a == name {
			return true
		}
	}
	return false
}

type SectionDef struct {
	Key         string          `json:"key"`
	Label       string          `json:"label"`
	HasBlock    bool            `json:"hasBlock"`
	Singleton   string          `json:"singleton,omitempty"`
	Collections []CollectionDef `json:"collections"`
}

type registry struct {
	SectionOrder []string               `json:"sectionOrder"`
	Sections     map[string]*SectionDef `json:"sections"`
	Footer       []CollectionDef        `json:"footer"`
	// Pages describes the inner pages (About, Leadership, Sector), which are
	// stored generically. See content.go.
	Pages map[string]*PageDef `json:"pages"`
}

//go:embed registry.json
var raw []byte

var reg registry

func init() {
	if err := json.Unmarshal(raw, &reg); err != nil {
		panic(fmt.Sprintf("registry.json is invalid: %v", err))
	}
}

func ResolveSection(key string) *SectionDef { return reg.Sections[key] }

// Sections returns the folds in page order.
func SectionByKey(key string) *SectionDef { return reg.Sections[key] }

// ResolveCollection is the single gate between a URL and a routine name.
// "footer" addresses the collections that hang off no fold.
func ResolveCollection(section, collection string) *CollectionDef {
	if section == "footer" {
		for i := range reg.Footer {
			if reg.Footer[i].Key == collection {
				return &reg.Footer[i]
			}
		}
		return nil
	}
	def := reg.Sections[section]
	if def == nil {
		return nil
	}
	for i := range def.Collections {
		if def.Collections[i].Key == collection {
			return &def.Collections[i]
		}
	}
	return nil
}
