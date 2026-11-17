package handlers

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"cms-backend/internal/apierr"
	"cms-backend/internal/auth"
	"cms-backend/internal/cms"
	"cms-backend/internal/db"
)

/* --- navigation ----------------------------------------------------------- */

func (h *Handler) NavMenus(c *gin.Context) error {
	rows, err := h.DB.Call(c.Request.Context(), "cms.nav_menu_list")
	if err != nil {
		return err
	}
	apierr.OK(c, rows)
	return nil
}

// NavList returns one menu's items, flat and in order. parent_id is on every
// row, so the screen builds the tree itself and reorder keeps working one
// level at a time. {items} matches the home-page collection lists.
func (h *Handler) NavList(c *gin.Context) error {
	rows, err := h.DB.Call(c.Request.Context(), "cms.nav_item_list", c.Param("menuKey"), c.Query("active") == "true")
	if err != nil {
		return err
	}
	apierr.OK(c, gin.H{"items": rows})
	return nil
}

func (h *Handler) NavCreate(c *gin.Context) error {
	body, err := readJSON(c)
	if err != nil {
		return err
	}
	r := cms.NewReader(body)
	label := r.String("label", cms.StrOpts{Required: true, Max: 200})
	href := r.Href("href", false)
	if href == "" {
		href = "#"
	}
	parent := r.ID("parent_id", false)
	icon := r.ID("icon_media_id", false)
	newTab := r.Bool("opens_in_new_tab", false)
	if err := r.Done(); err != nil {
		return err
	}
	row, err := h.DB.One(c.Request.Context(), "cms.nav_item_create", c.Param("menuKey"), label, href, parent, icon, newTab)
	if err != nil {
		return err
	}
	apierr.OK(c, row, 201)
	return nil
}

func (h *Handler) NavReorder(c *gin.Context) error {
	// Ordering is per level. Without ?parent the call means the top level,
	// which is what the routine's NULL default already says.
	var parent any
	if raw := c.Query("parent"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			return apierr.Validation(map[string]string{"parent": "Must be a valid id."}, "")
		}
		parent = id
	}
	body, err := readJSON(c)
	if err != nil {
		return err
	}
	r := cms.NewReader(body)
	ids := r.IDList("ids")
	if err := r.Done(); err != nil {
		return err
	}
	rows, err := h.DB.Call(c.Request.Context(), "cms.nav_item_reorder", c.Param("menuKey"), ids, parent)
	if err != nil {
		return err
	}
	apierr.OK(c, rows)
	return nil
}

func (h *Handler) NavGet(c *gin.Context) error {
	id, err := pathID(c, "id")
	if err != nil {
		return err
	}
	row, err := h.DB.One(c.Request.Context(), "cms.nav_item_get", id)
	if err != nil {
		return err
	}
	if row == nil {
		return apierr.NotFound("")
	}
	apierr.OK(c, row)
	return nil
}

func (h *Handler) NavUpdate(c *gin.Context) error {
	id, err := pathID(c, "id")
	if err != nil {
		return err
	}
	body, err := readJSON(c)
	if err != nil {
		return err
	}
	r := cms.NewReader(body)
	label := r.String("label", cms.StrOpts{Required: true, Max: 200})
	href := r.Href("href", false)
	if href == "" {
		href = "#"
	}
	icon := r.ID("icon_media_id", false)
	newTab := r.Bool("opens_in_new_tab", false)
	if err := r.Done(); err != nil {
		return err
	}
	row, err := h.DB.One(c.Request.Context(), "cms.nav_item_update", id, label, href, icon, newTab)
	if err != nil {
		return err
	}
	apierr.OK(c, row)
	return nil
}

func (h *Handler) NavDelete(c *gin.Context) error {
	id, err := pathID(c, "id")
	if err != nil {
		return err
	}
	// Children go with the parent: navigation_items cascades on parent_id.
	row, err := h.DB.One(c.Request.Context(), "cms.nav_item_delete", id)
	if err != nil {
		return err
	}
	if deleted, _ := db.Scalar(row).(bool); !deleted {
		return apierr.NotFound("That link no longer exists.")
	}
	apierr.OK(c, gin.H{"deleted": true})
	return nil
}

func (h *Handler) NavSetActive(c *gin.Context) error {
	id, err := pathID(c, "id")
	if err != nil {
		return err
	}
	body, err := readJSON(c)
	if err != nil {
		return err
	}
	r := cms.NewReader(body)
	active := r.Bool("is_active", false)
	if err := r.Done(); err != nil {
		return err
	}
	row, err := h.DB.One(c.Request.Context(), "cms.nav_item_set_active", id, active)
	if err != nil {
		return err
	}
	apierr.OK(c, row)
	return nil
}

// NavMove re-parents a link. A route of its own, for the same reason the
// routine is: parent_id null is a real value meaning "top level", so a body
// that omitted it could not be told apart from one asking to promote the
// link. The routine refuses a cycle, a different menu, or self-parenting.
func (h *Handler) NavMove(c *gin.Context) error {
	id, err := pathID(c, "id")
	if err != nil {
		return err
	}
	body, err := readJSON(c)
	if err != nil {
		return err
	}
	r := cms.NewReader(body)
	parent := r.ID("parent_id", false)
	if err := r.Done(); err != nil {
		return err
	}
	row, err := h.DB.One(c.Request.Context(), "cms.nav_item_move", id, parent)
	if err != nil {
		return err
	}
	apierr.OK(c, row)
	return nil
}

/* --- SEO and settings ----------------------------------------------------- */

func (h *Handler) SEOGet(c *gin.Context) error {
	slug := c.Param("slug")
	row, err := h.DB.One(c.Request.Context(), "cms.seo_get_by_page", slug)
	if err != nil {
		return err
	}
	if row == nil {
		return apierr.NotFound(`No SEO metadata for page "` + slug + `".`)
	}
	apierr.OK(c, row)
	return nil
}

func (h *Handler) SEOPut(c *gin.Context) error {
	body, err := readJSON(c)
	if err != nil {
		return err
	}
	r := cms.NewReader(body)
	s := func(k string, max int) string { return r.String(k, cms.StrOpts{Max: max}) }
	robots := s("robots", 100)
	if robots == "" {
		robots = "index,follow"
	}
	args := []any{
		c.Param("slug"),
		s("meta_title", 200), s("meta_description", 500), r.StringList("meta_keywords", 40),
		cms.Nullable(s("canonical_url", 500)), cms.Nullable(s("og_title", 200)), cms.Nullable(s("og_description", 500)),
		r.ID("og_image_media_id", false), robots,
	}
	if err := r.Done(); err != nil {
		return err
	}
	// seo_upsert raises no_data_found for an unknown page, which apierr maps to
	// 404, so an invented slug cannot quietly create metadata that belongs to
	// nothing.
	row, err := h.DB.One(c.Request.Context(), "cms.seo_upsert", args...)
	if err != nil {
		return err
	}
	apierr.OK(c, row)
	return nil
}

func (h *Handler) SettingsGet(c *gin.Context) error {
	row, err := h.DB.One(c.Request.Context(), "cms.site_settings_get")
	if err != nil {
		return err
	}
	if row == nil {
		return apierr.NotFound("Site settings have not been seeded yet.")
	}
	apierr.OK(c, row)
	return nil
}

func (h *Handler) SettingsPut(c *gin.Context) error {
	if u := auth.UserFrom(c); u == nil || u.Role != "admin" {
		return apierr.Forbidden("Administrator access required.")
	}
	body, err := readJSON(c)
	if err != nil {
		return err
	}
	r := cms.NewReader(body)
	args := []any{
		r.String("brand_name", cms.StrOpts{Required: true, Max: 120}),
		r.String("legal_name", cms.StrOpts{Required: true, Max: 200}),
		r.String("address", cms.StrOpts{Max: 500}),
		r.String("phone", cms.StrOpts{Max: 120}),
		r.String("email", cms.StrOpts{Max: 200}),
		r.String("search_placeholder", cms.StrOpts{Max: 200}),
		r.ID("logo_media_id", false),
		r.ID("footer_logo_media_id", false),
		r.String("connect_label", cms.StrOpts{Max: 120}),
		r.String("stocks_heading", cms.StrOpts{Max: 120}),
	}
	if err := r.Done(); err != nil {
		return err
	}
	row, err := h.DB.One(c.Request.Context(), "cms.site_settings_update", args...)
	if err != nil {
		return err
	}
	apierr.OK(c, row)
	return nil
}

/* --- public payloads ------------------------------------------------------ */

// PublicPage serves the published page as one document. The routine has
// already dropped inactive rows, applied banner scheduling and resolved media
// to URLs, so there is no shaping here that could diverge from what the
// dashboard previews.
func (h *Handler) PublicPage(c *gin.Context) error {
	slug := c.Param("slug")
	row, err := h.DB.One(c.Request.Context(), "cms.public_page_payload", slug)
	if err != nil {
		return err
	}
	// NULL for an unknown or disabled page: a 404, not an empty page.
	payload := db.Scalar(row)
	if row == nil || payload == nil {
		return apierr.NotFound(`No published page "` + slug + `".`)
	}
	apierr.OK(c, payload)
	return nil
}

// PublicSite serves the chrome every marketing route needs: settings, menus
// and footer content, separate from any one page's payload.
func (h *Handler) PublicSite(c *gin.Context) error {
	row, err := h.DB.One(c.Request.Context(), "cms.public_site_payload")
	if err != nil {
		return err
	}
	payload := db.Scalar(row)
	if row == nil || payload == nil {
		return apierr.NotFound("Site settings are not seeded.")
	}
	apierr.OK(c, payload)
	return nil
}
