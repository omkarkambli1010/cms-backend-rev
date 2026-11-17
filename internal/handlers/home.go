package handlers

import (
	"fmt"

	"github.com/gin-gonic/gin"

	"cms-backend/internal/apierr"
	"cms-backend/internal/cms"
	"cms-backend/internal/db"
)

/* --- the sections hub ----------------------------------------------------- */

// Sections is the home hub's list. Rows come from the database, but whether a
// fold has a dashboard screen, and what it is called, comes from the registry.
func (h *Handler) Sections(c *gin.Context) error {
	page := c.DefaultQuery("page", "home")
	rows, err := h.DB.Call(c.Request.Context(), "cms.section_list_by_page", page)
	if err != nil {
		return err
	}

	out := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		key := fmt.Sprint(row["section_key"])
		def := cms.SectionByKey(key)
		content := cms.ResolveContentSection(page, key)

		label, collections := row["name"], []gin.H{}
		if content != nil {
			label = content.Label
			for _, col := range content.Collections {
				collections = append(collections, gin.H{"key": col.Key, "label": col.Label})
			}
			def = nil
		}
		if def != nil {
			label = def.Label
			for _, col := range def.Collections {
				collections = append(collections, gin.H{"key": col.Key, "label": col.Label})
			}
		}
		out = append(out, gin.H{
			"id": toInt64(row["id"]), "key": key, "name": row["name"], "label": label,
			"order": row["display_order"], "isActive": row["is_active"], "updatedAt": row["updated_at"],
			"screen": def != nil || content != nil, "collections": collections,
		})
	}
	apierr.OK(c, out)
	return nil
}

func (h *Handler) SectionSetActive(c *gin.Context) error {
	id, err := pathID(c, "id")
	if err != nil {
		return err
	}
	body, err := readJSON(c)
	if err != nil {
		return err
	}
	r := cms.NewReader(body)
	if _, present := body["is_active"]; !present {
		r.Problem("is_active", "This field is required.")
	}
	active := r.Bool("is_active", false)
	if err := r.Done(); err != nil {
		return err
	}
	row, err := h.DB.One(c.Request.Context(), "cms.section_set_active", id, active)
	if err != nil {
		return err
	}
	apierr.OK(c, row)
	return nil
}

func (h *Handler) SectionsReorder(c *gin.Context) error {
	body, err := readJSON(c)
	if err != nil {
		return err
	}
	r := cms.NewReader(body)
	page := r.String("page", cms.StrOpts{})
	if page == "" {
		page = "home"
	}
	ids := r.IDList("ids")
	if err := r.Done(); err != nil {
		return err
	}
	// cms.section_reorder rejects a list that is not exactly this page's ids,
	// so a dashboard holding a stale list fails loudly instead of renumbering
	// half the page.
	rows, err := h.DB.Call(c.Request.Context(), "cms.section_reorder", page, ids)
	if err != nil {
		return err
	}
	apierr.OK(c, rows)
	return nil
}

/* --- one fold's shell ----------------------------------------------------- */

func (h *Handler) SectionGet(c *gin.Context) error {
	key := c.Param("section")
	if content := cms.ResolveContentSection(pageOf(c), key); content != nil {
		return h.contentSectionGet(c, content)
	}
	def := cms.ResolveSection(key)
	if def == nil {
		return apierr.NotFound("No such section: " + key + ".")
	}
	ctx := c.Request.Context()

	var block, singleton db.Row
	var err error
	if def.HasBlock {
		if block, err = h.DB.One(ctx, "cms.section_block_get", pageOf(c), key); err != nil {
			return err
		}
	}
	switch def.Singleton {
	case "hero":
		singleton, err = h.DB.One(ctx, "cms.hero_get", pageOf(c))
	case "sticky-nudge":
		singleton, err = h.DB.One(ctx, "cms.sticky_nudge_get", pageOf(c), key)
	}
	if err != nil {
		return err
	}

	collections := make([]map[string]any, 0, len(def.Collections))
	for i := range def.Collections {
		collections = append(collections, def.Collections[i].Public())
	}

	var singletonKind any
	if def.Singleton != "" {
		singletonKind = def.Singleton
	}
	apierr.OK(c, gin.H{
		"key": def.Key, "label": def.Label, "hasBlock": def.HasBlock, "singletonKind": singletonKind,
		"collections": collections, "block": block, "singleton": singleton,
	})
	return nil
}

func (h *Handler) SectionPut(c *gin.Context) error {
	key := c.Param("section")
	if content := cms.ResolveContentSection(pageOf(c), key); content != nil {
		return h.contentSectionPut(c, content)
	}
	def := cms.ResolveSection(key)
	if def == nil {
		return apierr.NotFound("No such section: " + key + ".")
	}
	body, err := readJSON(c)
	if err != nil {
		return err
	}
	r := cms.NewReader(body)
	s := func(k string, max int) string { return r.String(k, cms.StrOpts{Max: max}) }
	ctx := c.Request.Context()

	switch def.Singleton {
	case "hero":
		args := []any{
			r.ID("id", true),
			s("title_lead", 200), s("subtext_line_1", 300), s("subtext_line_2", 300),
			s("watermark_left", 60), s("watermark_right", 60),
			r.ID("mockup_media_id", false),
			s("form_name_label", 120), s("form_name_placeholder", 200),
			s("form_mobile_label", 120), s("form_mobile_placeholder", 200),
			s("form_mobile_help", 300), s("form_consent_label", 300), s("form_submit_label", 120),
			s("form_name_label_mobile", 120), s("form_name_placeholder_mobile", 200),
			s("form_mobile_label_mobile", 120), s("form_mobile_placeholder_mobile", 200),
		}
		if err := r.Done(); err != nil {
			return err
		}
		row, err := h.DB.One(ctx, "cms.hero_update", args...)
		if err != nil {
			return err
		}
		apierr.OK(c, row)
		return nil

	case "sticky-nudge":
		args := []any{
			pageOf(c),
			s("title", 200), s("mobile_placeholder", 200), s("name_placeholder", 200), s("cta_label", 120),
			cms.Nullable(r.Href("cta_href", false)),
			key,
		}
		if err := r.Done(); err != nil {
			return err
		}
		row, err := h.DB.One(ctx, "cms.sticky_nudge_update", args...)
		if err != nil {
			return err
		}
		apierr.OK(c, row)
		return nil
	}

	if !def.HasBlock {
		return apierr.NotFound("Section " + key + " has no editable shell.")
	}

	align := s("align", 0)
	if align == "" {
		align = "start"
	}
	tone := s("tone", 0)
	if tone == "" {
		tone = "light"
	}
	args := []any{
		pageOf(c), key,
		s("eyebrow", 120), s("title", 300), r.StringList("title_lines", 8),
		s("subtitle", 500), s("subtitle_mobile", 500), s("description", 2000),
		r.ID("title_break_after", false), r.ID("subtitle_break_after", false),
		align, tone,
		s("cta_label", 120), s("cta_label_mobile", 120),
		cms.Nullable(r.Href("cta_href", false)), r.Bool("cta_visible", true),
		s("secondary_cta_label", 120), cms.Nullable(r.Href("secondary_cta_href", false)),
		r.Bool("secondary_cta_visible", false),
		cms.Nullable(r.Href("arrow_href", false)), s("arrow_label", 120),
		r.ID("image_media_id", false), r.ID("image_mobile_media_id", false),
		r.ID("background_media_id", false), r.ID("background_mobile_media_id", false),
		s("background_color", 7),
	}
	if err := r.Done(); err != nil {
		return err
	}
	row, err := h.DB.One(ctx, "cms.section_block_update", args...)
	if err != nil {
		return err
	}
	apierr.OK(c, row)
	return nil
}

/* --- ordered collections -------------------------------------------------- */

// CollectionList serves one handler for twenty-odd collections because they
// all expose the same operations under the same names. What differs (the
// routine prefix, the parent, the argument order) is data in the registry.
func (h *Handler) CollectionList(c *gin.Context) error {
	if content := cms.ResolveContentSection(pageOf(c), c.Param("section")); content != nil {
		return h.contentList(c, content)
	}
	def, err := collectionFor(c)
	if err != nil {
		return err
	}
	parent, err := h.resolveParentID(c, def)
	if err != nil {
		return err
	}
	onlyActive := c.Query("active") == "true"

	args := []any{onlyActive}
	if parent != nil {
		args = []any{parent, onlyActive}
	}
	items, err := h.DB.Call(c.Request.Context(), fn(def, "list"), args...)
	if err != nil {
		return err
	}
	apierr.OK(c, gin.H{"collection": def.Public(), "parentId": parent, "items": items})
	return nil
}

func (h *Handler) CollectionCreate(c *gin.Context) error {
	if content := cms.ResolveContentSection(pageOf(c), c.Param("section")); content != nil {
		return h.contentCreate(c, content)
	}
	def, err := collectionFor(c)
	if err != nil {
		return err
	}
	body, err := readJSON(c)
	if err != nil {
		return err
	}
	parent, err := h.resolveParentID(c, def)
	if err != nil {
		return err
	}
	args, err := cms.BuildArgs(def, body, "create")
	if err != nil {
		return err
	}
	if parent != nil {
		args = append([]any{parent}, args...)
	}
	row, err := h.DB.One(c.Request.Context(), fn(def, "create"), args...)
	if err != nil {
		return err
	}
	apierr.OK(c, row, 201)
	return nil
}

func (h *Handler) CollectionReorder(c *gin.Context) error {
	if content := cms.ResolveContentSection(pageOf(c), c.Param("section")); content != nil {
		return h.contentReorder(c, content)
	}
	def, err := collectionFor(c)
	if err != nil {
		return err
	}
	parent, err := h.resolveParentID(c, def)
	if err != nil {
		return err
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
	// The routine rejects an id set that is not exactly the parent's current
	// one, so a dashboard that has drifted fails the whole reorder rather than
	// renumbering the rows it still knows about.
	args := []any{ids}
	if parent != nil {
		args = []any{parent, ids}
	}
	rows, err := h.DB.Call(c.Request.Context(), fn(def, "reorder"), args...)
	if err != nil {
		return err
	}
	apierr.OK(c, rows)
	return nil
}

func (h *Handler) CollectionGet(c *gin.Context) error {
	if content := cms.ResolveContentSection(pageOf(c), c.Param("section")); content != nil {
		return h.contentGet(c, content)
	}
	def, err := collectionFor(c)
	if err != nil {
		return err
	}
	id, err := pathID(c, "id")
	if err != nil {
		return err
	}
	row, err := h.DB.One(c.Request.Context(), fn(def, "get"), id)
	if err != nil {
		return err
	}
	if row == nil {
		return apierr.NotFound("")
	}
	apierr.OK(c, row)
	return nil
}

func (h *Handler) CollectionUpdate(c *gin.Context) error {
	if content := cms.ResolveContentSection(pageOf(c), c.Param("section")); content != nil {
		return h.contentUpdate(c, content)
	}
	def, err := collectionFor(c)
	if err != nil {
		return err
	}
	id, err := pathID(c, "id")
	if err != nil {
		return err
	}
	body, err := readJSON(c)
	if err != nil {
		return err
	}
	args, err := cms.BuildArgs(def, body, "update")
	if err != nil {
		return err
	}
	row, err := h.DB.One(c.Request.Context(), fn(def, "update"), append([]any{id}, args...)...)
	if err != nil {
		return err
	}
	apierr.OK(c, row)
	return nil
}

func (h *Handler) CollectionDelete(c *gin.Context) error {
	if content := cms.ResolveContentSection(pageOf(c), c.Param("section")); content != nil {
		return h.contentDelete(c, content)
	}
	def, err := collectionFor(c)
	if err != nil {
		return err
	}
	id, err := pathID(c, "id")
	if err != nil {
		return err
	}
	row, err := h.DB.One(c.Request.Context(), fn(def, "delete"), id)
	if err != nil {
		return err
	}
	// The delete routines return a plain boolean under the routine's own
	// name; read the single value rather than the key.
	if deleted, _ := db.Scalar(row).(bool); !deleted {
		return apierr.NotFound("That row no longer exists.")
	}
	apierr.OK(c, gin.H{"deleted": true})
	return nil
}

// CollectionSetActive is the publication switch. is_active is the only
// publication control there is (there is no draft state), so this is how a
// row leaves the page.
func (h *Handler) CollectionSetActive(c *gin.Context) error {
	if content := cms.ResolveContentSection(pageOf(c), c.Param("section")); content != nil {
		return h.contentSetActive(c, content)
	}
	def, err := collectionFor(c)
	if err != nil {
		return err
	}
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
	row, err := h.DB.One(c.Request.Context(), fn(def, "set_active"), id, active)
	if err != nil {
		return err
	}
	apierr.OK(c, row)
	return nil
}

// CollectionSetLead promotes one story to the lead slot. A route of its own
// because the lead is exclusive: the routine demotes the current lead in the
// same statement, so the digest never renders two lead cards or none.
func (h *Handler) CollectionSetLead(c *gin.Context) error {
	def, err := collectionFor(c)
	if err != nil {
		return err
	}
	if !def.HasAction("set_lead") {
		return apierr.NotFound(fmt.Sprintf("%s/%s has no lead item.", c.Param("section"), c.Param("collection")))
	}
	id, err := pathID(c, "id")
	if err != nil {
		return err
	}
	row, err := h.DB.One(c.Request.Context(), fn(def, "set_lead"), id)
	if err != nil {
		return err
	}
	apierr.OK(c, row)
	return nil
}
