package cms

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"cms-backend/internal/apierr"
)

var (
	slugRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	dateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

type coerced struct {
	value   any
	problem string
}

func coerce(f FieldDef, raw any) coerced {
	switch f.Kind {
	case "text", "textarea":
		s, _ := raw.(string)
		s = strings.TrimSpace(s)
		if f.Required && s == "" {
			return coerced{s, "This field is required."}
		}
		if f.Max > 0 && utf8.RuneCountInString(s) > f.Max {
			return coerced{s, fmt.Sprintf("Must be %d characters or fewer.", f.Max)}
		}
		return coerced{value: s}

	case "href":
		s, _ := raw.(string)
		s = strings.TrimSpace(s)
		if s == "" {
			if f.Required {
				return coerced{s, "This field is required."}
			}
			return coerced{value: nil}
		}
		if p := checkHref(s, true); p != "" {
			return coerced{s, p}
		}
		return coerced{value: s}

	case "slug":
		s, _ := raw.(string)
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" {
			if f.Required {
				return coerced{s, "This field is required."}
			}
			return coerced{value: nil}
		}
		if !slugRe.MatchString(s) {
			return coerced{s, "Use lowercase letters, numbers and hyphens."}
		}
		return coerced{value: s}

	case "select":
		s, _ := raw.(string)
		s = strings.TrimSpace(s)
		if s == "" && !f.Required {
			return coerced{value: nil}
		}
		for _, o := range f.Options {
			if o == s {
				return coerced{value: s}
			}
		}
		return coerced{s, "Choose one of: " + strings.Join(f.Options, ", ") + "."}

	case "multiselect":
		// Several options at once; stored as the list of chosen option strings.
		out := []string{}
		if raw == nil {
			return coerced{value: out}
		}
		list, ok := raw.([]any)
		if !ok {
			return coerced{out, "Expected a list of choices."}
		}
		allowed := map[string]bool{}
		for _, o := range f.Options {
			allowed[o] = true
		}
		seen := map[string]bool{}
		for _, e := range list {
			s, _ := e.(string)
			s = strings.TrimSpace(s)
			if s == "" || seen[s] {
				continue
			}
			if !allowed[s] {
				return coerced{out, "Choose from: " + strings.Join(f.Options, ", ") + "."}
			}
			seen[s] = true
			out = append(out, s)
		}
		return coerced{value: out}

	case "lines":
		out := []string{}
		if raw == nil {
			return coerced{value: out}
		}
		list, ok := raw.([]any)
		if !ok {
			return coerced{out, "Expected a list of lines."}
		}
		for _, e := range list {
			if s, ok := e.(string); ok {
				if s = strings.TrimSpace(s); s != "" {
					out = append(out, s)
				}
			}
		}
		return coerced{value: out}

	case "number":
		if raw == nil || raw == "" {
			if f.Required {
				return coerced{nil, "This field is required."}
			}
			return coerced{value: nil}
		}
		var n float64
		switch x := raw.(type) {
		case float64:
			n = x
		case string:
			p, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
			if err != nil {
				return coerced{nil, "Must be a number."}
			}
			n = p
		case bool:
			if x {
				n = 1
			}
		default:
			return coerced{nil, "Must be a number."}
		}
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return coerced{nil, "Must be a number."}
		}
		// The routines take integer columns; a whole number goes as int64 so
		// the driver does not have to coerce a float.
		if n == math.Trunc(n) {
			return coerced{value: int64(n)}
		}
		return coerced{value: n}

	case "boolean":
		b, _ := raw.(bool)
		return coerced{value: b}

	case "media":
		if raw == nil || raw == "" {
			return coerced{value: nil}
		}
		id, ok := toInt(raw)
		if !ok {
			return coerced{nil, "Pick an image from the library."}
		}
		return coerced{value: id}

	case "date":
		if raw == nil || raw == "" {
			return coerced{value: nil}
		}
		s := strings.TrimSpace(fmt.Sprint(raw))
		if !dateRe.MatchString(s) {
			return coerced{nil, "Use the date picker."}
		}
		d, err := time.Parse("2006-01-02", s)
		if err != nil {
			return coerced{nil, "Use the date picker."}
		}
		return coerced{value: d}

	case "datetime":
		if raw == nil || raw == "" {
			return coerced{value: nil}
		}
		s := strings.TrimSpace(fmt.Sprint(raw))
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return coerced{value: t.UTC()}
		}
		// A datetime-local input sends no zone; read it in the server's local
		// zone, as the Node app did.
		for _, layout := range []string{"2006-01-02T15:04", "2006-01-02T15:04:05", "2006-01-02 15:04", "2006-01-02"} {
			if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
				return coerced{value: t.UTC()}
			}
		}
		return coerced{nil, "Not a valid date and time."}
	}
	return coerced{value: nil}
}

// implicit argument values: display_order and is_lead sit in an argument list
// but are not editable fields. Order is decided by where a row is appended
// (and changed by the reorder endpoint), and the lead story is set through its
// own call so two rows cannot both claim the slot.
func implicit(name string) (any, bool) {
	switch name {
	case "display_order":
		return nil, true // the routine appends to the end
	case "is_lead":
		return false, true
	}
	return nil, false
}

// BuildArgs turns a JSON body into the positional argument list a routine
// takes. Nothing here builds SQL text.
func BuildArgs(def *CollectionDef, body map[string]any, mode string) ([]any, error) {
	order := def.CreateArgs
	if mode == "update" {
		order = def.UpdateArgs
	}

	problems := map[string]string{}
	args := make([]any, 0, len(order))

	for _, name := range order {
		var field *FieldDef
		for i := range def.Fields {
			if def.Fields[i].Name == name {
				field = &def.Fields[i]
				break
			}
		}
		if field == nil {
			if v, ok := implicit(name); ok {
				args = append(args, v)
				continue
			}
			// A registry bug, not a bad request; guessing would write
			// something nobody chose.
			return nil, fmt.Errorf("registry lists argument %q for %s with no field", name, def.FnPrefix)
		}
		c := coerce(*field, body[field.Name])
		if c.problem != "" {
			problems[field.Name] = c.problem
		}
		args = append(args, c.value)
	}

	if len(problems) > 0 {
		return nil, apierr.Validation(problems, "")
	}
	return args, nil
}
