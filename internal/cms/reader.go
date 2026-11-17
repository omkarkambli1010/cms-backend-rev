package cms

import (
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"cms-backend/internal/apierr"
)

// Reader pulls typed values out of a JSON body. It collects problems rather
// than failing on the first, so a form reports every bad field at once.
type Reader struct {
	body     map[string]any
	problems map[string]string
}

func NewReader(body map[string]any) *Reader {
	return &Reader{body: body, problems: map[string]string{}}
}

type StrOpts struct {
	Required bool
	Max      int
}

func (r *Reader) String(key string, o StrOpts) string {
	v, _ := r.body[key].(string)
	v = strings.TrimSpace(v)

	if o.Required && v == "" {
		r.problems[key] = "This field is required."
		return ""
	}
	// Length is counted in UTF-16-ish characters by the dashboard; runes are
	// the closest honest measure here.
	if o.Max > 0 && utf8.RuneCountInString(v) > o.Max {
		r.problems[key] = fmt.Sprintf("Must be %d characters or fewer.", o.Max)
	}
	return v
}

var linkSchemes = map[string]bool{"http": true, "https": true, "mailto": true, "tel": true}

// checkHref returns a problem string, or "" when the link is acceptable. It
// rejects rather than sanitises: an editor who types javascript:... should be
// told the field is wrong, not have it silently rewritten.
func checkHref(value string, strict bool) string {
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "#") || (strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "//")) {
		return ""
	}
	if strings.HasPrefix(value, "//") {
		// Protocol-relative is an absolute URL in disguise.
		return "Start the link with / for a page on this site, or https:// for another."
	}
	base, _ := url.Parse("https://sbisecurities.in")
	u, err := url.Parse(value)
	if err != nil {
		return "That is not a valid link."
	}
	u = base.ResolveReference(u)
	if !linkSchemes[strings.ToLower(u.Scheme)] {
		return "Links must be a path, or use http, https, mailto or tel. That scheme is not allowed."
	}
	return ""
}

func (r *Reader) Href(key string, required bool) string {
	v := r.String(key, StrOpts{Required: required, Max: 500})
	if p := checkHref(v, true); p != "" {
		r.problems[key] = p
	}
	return v
}

func (r *Reader) Bool(key string, fallback bool) bool {
	if b, ok := r.body[key].(bool); ok {
		return b
	}
	return fallback
}

// toInt converts a JSON value the way Number() would, then requires a
// positive integer.
func toInt(raw any) (int64, bool) {
	var f float64
	switch x := raw.(type) {
	case float64:
		f = x
	case string:
		p, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		if err != nil {
			return 0, false
		}
		f = p
	default:
		return 0, false
	}
	if math.IsNaN(f) || math.IsInf(f, 0) || f != math.Trunc(f) || f <= 0 {
		return 0, false
	}
	return int64(f), true
}

// ID is a positive integer id, or nil.
func (r *Reader) ID(key string, required bool) any {
	raw, present := r.body[key]
	if !present || raw == nil || raw == "" {
		if required {
			r.problems[key] = "This field is required."
		}
		return nil
	}
	id, ok := toInt(raw)
	if !ok {
		r.problems[key] = "Must be a valid id."
		return nil
	}
	return id
}

func (r *Reader) IDList(key string) []int64 {
	list, ok := r.body[key].([]any)
	if !ok || len(list) == 0 {
		r.problems[key] = "Expected a non-empty list of ids."
		return []int64{}
	}
	out := make([]int64, 0, len(list))
	for _, e := range list {
		id, ok := toInt(e)
		if !ok {
			r.problems[key] = "List contains an invalid id."
			return []int64{}
		}
		out = append(out, id)
	}
	return out
}

func (r *Reader) StringList(key string, max int) []string {
	out := []string{}
	raw, present := r.body[key]
	if !present || raw == nil {
		return out
	}
	list, ok := raw.([]any)
	if !ok {
		r.problems[key] = "Expected a list."
		return out
	}
	for _, e := range list {
		if s, ok := e.(string); ok {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
	}
	if max > 0 && len(out) > max {
		r.problems[key] = fmt.Sprintf("At most %d entries.", max)
	}
	return out
}

func (r *Reader) Problem(key, msg string) { r.problems[key] = msg }

// Done returns a validation error carrying everything collected, or nil.
func (r *Reader) Done() error {
	if len(r.problems) > 0 {
		return apierr.Validation(r.problems, "")
	}
	return nil
}

// Nullable turns an empty string into SQL NULL.
func Nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
