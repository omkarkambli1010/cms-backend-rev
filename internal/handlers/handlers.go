// Package handlers holds the HTTP endpoints. Each one authenticates, validates,
// and then calls stored routines through the db package; none of them writes
// SQL.
package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"cms-backend/internal/apierr"
	"cms-backend/internal/auth"
	"cms-backend/internal/cms"
	"cms-backend/internal/config"
	"cms-backend/internal/db"
	"cms-backend/internal/media"
)

type Handler struct {
	Cfg      *config.Config
	DB       *db.DB
	Auth     *auth.Service
	Media    *media.Storage
	Throttle *auth.Throttle
}

// maxJSONBytes bounds a request body. Every JSON payload here is a form.
const maxJSONBytes = 1 << 20

func readJSON(c *gin.Context) (map[string]any, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxJSONBytes)
	data, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return nil, apierr.Validation(nil, "Request body is not valid JSON.")
	}
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil || body == nil {
		if err == nil {
			return nil, apierr.Validation(nil, "Request body must be a JSON object.")
		}
		return nil, apierr.Validation(nil, "Request body is not valid JSON.")
	}
	return body, nil
}

// pathID guards ids taken from the URL the same way body ids are guarded.
func pathID(c *gin.Context, name string) (int64, error) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id <= 0 {
		return 0, apierr.Validation(map[string]string{name: "Must be a valid id."}, "Invalid "+name+".")
	}
	return id, nil
}

func toInt64(v any) int64 {
	n, _ := strconv.ParseInt(fmt.Sprint(v), 10, 64)
	return n
}

// handle adapts a function returning an error to a gin handler, so every
// failure lands in apierr.Fail.

func collectionFor(c *gin.Context) (*cms.CollectionDef, error) {
	section, collection := c.Param("section"), c.Param("collection")
	def := cms.ResolveCollection(section, collection)
	if def == nil {
		return nil, apierr.NotFound(fmt.Sprintf("No such collection: %s/%s.", section, collection))
	}
	return def, nil
}

// pageOf is the page a request addresses. The /admin/pages/:page routes carry
// it in the path; the older /admin/home routes are the same handlers with the
// page fixed, so neither has to know about the other.
func pageOf(c *gin.Context) string {
	if p := c.Param("page"); p != "" {
		return p
	}
	return "home"
}

func fn(def *cms.CollectionDef, verb string) string {
	return "cms." + def.FnPrefix + "_" + verb
}

// resolveParentID finds the id a _list, _create or _reorder hangs off.
//
// "section" is derived from the URL, so a caller cannot point a fold's rows at
// another fold. The nested kinds take an id from the query string, because
// only the caller knows which tab is open; the foreign key keeps them honest,
// and every caller is an authenticated user.
func (h *Handler) resolveParentID(c *gin.Context, def *cms.CollectionDef) (any, error) {
	switch def.Parent {
	case "none":
		return nil, nil

	case "section":
		section := c.Param("section")
		row, err := h.DB.One(c.Request.Context(), "cms.section_id_for", pageOf(c), section)
		if err != nil {
			return nil, err
		}
		if row == nil || db.Scalar(row) == nil {
			return nil, apierr.NotFound(fmt.Sprintf("No section %q on the %s page.", section, pageOf(c)))
		}
		return toInt64(db.Scalar(row)), nil

	case "hero":
		row, err := h.DB.One(c.Request.Context(), "cms.hero_get", pageOf(c))
		if err != nil {
			return nil, err
		}
		if row == nil {
			return nil, apierr.NotFound("This page has no hero yet.")
		}
		return toInt64(row["id"]), nil
	}

	id, err := strconv.ParseInt(strings.TrimSpace(c.Query("parent")), 10, 64)
	if err != nil || id <= 0 {
		return nil, apierr.Validation(map[string]string{"parent": "Required."},
			fmt.Sprintf("This list belongs to a %s; pass ?parent=<id>.", def.Parent))
	}
	return id, nil
}

/* --- auth ----------------------------------------------------------------- */

func (h *Handler) Login(c *gin.Context) error {
	body, err := readJSON(c)
	if err != nil {
		return err
	}
	r := cms.NewReader(body)
	username := r.String("username", cms.StrOpts{Required: true, Max: 20})
	password := r.String("password", cms.StrOpts{Required: true, Max: 200})
	if err := r.Done(); err != nil {
		return err
	}

	// Staff codes: digits only, as admin_users_username_check enforces.
	if len(username) < 4 || len(username) > 20 || strings.Trim(username, "0123456789") != "" {
		return apierr.Validation(map[string]string{"username": "Your username is digits only."}, "")
	}

	ip := auth.ClientIP(c)
	if ip == "" {
		ip = "local"
	}
	key := username + "|" + ip
	if !h.Throttle.Hit(key) {
		return apierr.Unauthorized("Too many attempts. Try again in a few minutes.")
	}

	user, err := h.DB.One(c.Request.Context(), "cms.admin_user_get_by_username", username)
	if err != nil {
		return err
	}

	// The same message and the same amount of work whether the username is
	// unknown, the account is switched off, or the password is wrong: three
	// different answers would be an account-enumeration oracle. The hash is
	// verified against a dummy record when there is no user so timing does not
	// give the answer away either.
	hash, salt, active := strings.Repeat("0", 128), strings.Repeat("0", 32), false
	if user != nil {
		hash, salt = fmt.Sprint(user["password_hash"]), fmt.Sprint(user["password_salt"])
		active, _ = user["is_active"].(bool)
	}
	correct := auth.VerifyPassword(password, hash, salt)

	if user == nil || !active || !correct {
		return apierr.Validation(map[string]string{"password": "Check your username and password."},
			"Those details do not match an active account.")
	}

	h.Throttle.Clear(key)
	id := toInt64(user["id"])

	if err := h.Auth.CreateSession(c, id); err != nil {
		return err
	}
	if _, err := h.DB.Call(c.Request.Context(), "cms.admin_user_touch_login", id); err != nil {
		return err
	}

	apierr.OK(c, gin.H{
		"id": id, "username": user["username"], "fullName": user["full_name"], "role": user["role"],
	})
	return nil
}

func (h *Handler) Logout(c *gin.Context) error {
	h.Auth.DestroySession(c)
	apierr.OK(c, gin.H{"signedOut": true})
	return nil
}

// Me tells the Next.js server-rendered screens who is signed in. A signed-out
// caller is a normal answer here, not an error: the layout redirects on it.
func (h *Handler) Me(c *gin.Context) error {
	u, err := h.Auth.Resolve(c.Request.Context(), c)
	if err != nil {
		// A database outage reads as "not signed in" rather than a 500 on
		// every page, so the dashboard can still render its error state.
		apierr.OK(c, gin.H{"user": nil, "bypassed": false})
		return nil
	}
	if u == nil {
		apierr.OK(c, gin.H{"user": nil, "bypassed": false})
		return nil
	}
	apierr.OK(c, gin.H{"user": u, "bypassed": h.Auth.Bypassed()})
	return nil
}

/* --- overview ------------------------------------------------------------- */

func (h *Handler) Overview(c *gin.Context) error {
	user := auth.UserFrom(c)

	// The health check runs first and is reported rather than thrown: an
	// Overview that says "database unreachable" beats one that 500s.
	healthy := h.DB.Ping(c.Request.Context())
	counts := []gin.H{}
	if healthy {
		rows, err := h.DB.Call(c.Request.Context(), "cms.overview_counts")
		if err != nil {
			return err
		}
		for _, row := range rows {
			counts = append(counts, gin.H{
				"label": row["label"], "total": toInt64(row["total"]), "inactive": toInt64(row["inactive"]),
			})
		}
	}

	apierr.OK(c, gin.H{
		"user":     gin.H{"fullName": user.FullName, "username": user.Username, "role": user.Role},
		"database": gin.H{"healthy": healthy},
		"counts":   counts,
	})
	return nil
}
