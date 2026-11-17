package handlers

import (
	"fmt"

	"github.com/gin-gonic/gin"

	"cms-backend/internal/apierr"
	"cms-backend/internal/cms"
	"cms-backend/internal/db"
)

// Handlers for the sections of the inner pages (About, Leadership, Sector).
//
// They answer the same routes as the home folds and in the same shapes, so the
// dashboard's section and collection screens serve both: a row comes back flat
// (its fields beside id / is_active / display_order) and goes in flat. What is
// different is storage: one JSON object per section and one per row, validated
// against the registry's field list and handed to the generic routines in
// migration 018.

// contentSectionID resolves the section row a request addresses.
func (h *Handler) contentSectionID(c *gin.Context, section string) (int64, error) {
	row, err := h.DB.One(c.Request.Context(), "cms.section_id_for", pageOf(c), section)
	if err != nil {
		return 0, err
	}
	if row == nil || db.Scalar(row) == nil {
		return 0, apierr.NotFound(fmt.Sprintf("No section %q on the %s page.", section, pageOf(c)))
	}
	return toInt64(db.Scalar(row)), nil
}

func (h *Handler) contentCollectionFor(c *gin.Context, s *cms.ContentSection) (*cms.ContentCollection, error) {
	col := s.Collection(c.Param("collection"))
	if col == nil {
		return nil, apierr.NotFound(fmt.Sprintf("No such collection: %s/%s.", s.Key, c.Param("collection")))
	}
	return col, nil
}

/* --- the section's own block --------------------------------------------- */

func (h *Handler) contentSectionGet(c *gin.Context, s *cms.ContentSection) error {
	row, err := h.DB.One(c.Request.Context(), "cms.content_block_get", pageOf(c), s.Key)
	if err != nil {
		return err
	}
	if row == nil {
		return apierr.NotFound("No such section: " + s.Key + ".")
	}

	collections := make([]map[string]any, 0, len(s.Collections))
	for i := range s.Collections {
		collections = append(collections, s.Collections[i].Public())
	}

	block, _ := row["data"].(map[string]any)
	if block == nil {
		block = map[string]any{}
	}
	apierr.OK(c, gin.H{
		"key": s.Key, "label": s.Label, "kind": "content",
		"hasBlock": len(s.Block) > 0, "blockFields": s.Block,
		"collections": collections, "block": block,
	})
	return nil
}

func (h *Handler) contentSectionPut(c *gin.Context, s *cms.ContentSection) error {
	if len(s.Block) == 0 {
		return apierr.NotFound("Section " + s.Key + " has no fields of its own.")
	}
	body, err := readJSON(c)
	if err != nil {
		return err
	}
	data, err := cms.BuildContentData(s.Block, body)
	if err != nil {
		return err
	}
	row, err := h.DB.One(c.Request.Context(), "cms.content_block_update", pageOf(c), s.Key, data)
	if err != nil {
		return err
	}
	apierr.OK(c, row)
	return nil
}

/* --- collections ---------------------------------------------------------- */

func (h *Handler) contentList(c *gin.Context, s *cms.ContentSection) error {
	col, err := h.contentCollectionFor(c, s)
	if err != nil {
		return err
	}
	sectionID, err := h.contentSectionID(c, s.Key)
	if err != nil {
		return err
	}
	rows, err := h.DB.Call(c.Request.Context(), "cms.content_item_list", sectionID, col.Key, c.Query("active") == "true")
	if err != nil {
		return err
	}
	items := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		items = append(items, cms.FlattenItem(r, col))
	}
	apierr.OK(c, gin.H{"collection": col.Public(), "parentId": sectionID, "items": items})
	return nil
}

func (h *Handler) contentCreate(c *gin.Context, s *cms.ContentSection) error {
	col, err := h.contentCollectionFor(c, s)
	if err != nil {
		return err
	}
	body, err := readJSON(c)
	if err != nil {
		return err
	}
	data, err := cms.BuildContentData(col.Fields, body)
	if err != nil {
		return err
	}
	sectionID, err := h.contentSectionID(c, s.Key)
	if err != nil {
		return err
	}
	// display_order is nil: the routine appends to the end of the collection.
	row, err := h.DB.One(c.Request.Context(), "cms.content_item_create", sectionID, col.Key, cms.ItemKey(col, data), data, nil)
	if err != nil {
		return err
	}
	apierr.OK(c, cms.FlattenItem(row, col), 201)
	return nil
}

func (h *Handler) contentReorder(c *gin.Context, s *cms.ContentSection) error {
	col, err := h.contentCollectionFor(c, s)
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
	sectionID, err := h.contentSectionID(c, s.Key)
	if err != nil {
		return err
	}
	rows, err := h.DB.Call(c.Request.Context(), "cms.content_item_reorder", sectionID, col.Key, ids)
	if err != nil {
		return err
	}
	items := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		items = append(items, cms.FlattenItem(r, col))
	}
	apierr.OK(c, items)
	return nil
}

// ownItem loads a row and refuses it unless it belongs to the collection the
// URL names, so /pages/about/about-values/values/12 cannot reach a leader.
func (h *Handler) ownItem(c *gin.Context, s *cms.ContentSection, col *cms.ContentCollection) (db.Row, int64, error) {
	id, err := pathID(c, "id")
	if err != nil {
		return nil, 0, err
	}
	sectionID, err := h.contentSectionID(c, s.Key)
	if err != nil {
		return nil, 0, err
	}
	row, err := h.DB.One(c.Request.Context(), "cms.content_item_get", id)
	if err != nil {
		return nil, 0, err
	}
	if row == nil || toInt64(row["section_id"]) != sectionID || fmt.Sprint(row["collection"]) != col.Key {
		return nil, 0, apierr.NotFound("")
	}
	return row, id, nil
}

func (h *Handler) contentGet(c *gin.Context, s *cms.ContentSection) error {
	col, err := h.contentCollectionFor(c, s)
	if err != nil {
		return err
	}
	row, _, err := h.ownItem(c, s, col)
	if err != nil {
		return err
	}
	apierr.OK(c, cms.FlattenItem(row, col))
	return nil
}

func (h *Handler) contentUpdate(c *gin.Context, s *cms.ContentSection) error {
	col, err := h.contentCollectionFor(c, s)
	if err != nil {
		return err
	}
	if _, _, err := h.ownItem(c, s, col); err != nil {
		return err
	}
	id, _ := pathID(c, "id")
	body, err := readJSON(c)
	if err != nil {
		return err
	}
	data, err := cms.BuildContentData(col.Fields, body)
	if err != nil {
		return err
	}
	row, err := h.DB.One(c.Request.Context(), "cms.content_item_update", id, cms.ItemKey(col, data), data)
	if err != nil {
		return err
	}
	apierr.OK(c, cms.FlattenItem(row, col))
	return nil
}

func (h *Handler) contentDelete(c *gin.Context, s *cms.ContentSection) error {
	col, err := h.contentCollectionFor(c, s)
	if err != nil {
		return err
	}
	if _, _, err := h.ownItem(c, s, col); err != nil {
		return err
	}
	id, _ := pathID(c, "id")
	row, err := h.DB.One(c.Request.Context(), "cms.content_item_delete", id)
	if err != nil {
		return err
	}
	if deleted, _ := db.Scalar(row).(bool); !deleted {
		return apierr.NotFound("That row no longer exists.")
	}
	apierr.OK(c, gin.H{"deleted": true})
	return nil
}

func (h *Handler) contentSetActive(c *gin.Context, s *cms.ContentSection) error {
	col, err := h.contentCollectionFor(c, s)
	if err != nil {
		return err
	}
	if _, _, err := h.ownItem(c, s, col); err != nil {
		return err
	}
	id, _ := pathID(c, "id")
	body, err := readJSON(c)
	if err != nil {
		return err
	}
	r := cms.NewReader(body)
	active := r.Bool("is_active", false)
	if err := r.Done(); err != nil {
		return err
	}
	row, err := h.DB.One(c.Request.Context(), "cms.content_item_set_active", id, active)
	if err != nil {
		return err
	}
	apierr.OK(c, cms.FlattenItem(row, col))
	return nil
}
