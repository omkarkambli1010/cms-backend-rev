package handlers

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"cms-backend/internal/apierr"
	"cms-backend/internal/auth"
	"cms-backend/internal/cms"
	"cms-backend/internal/db"
)

func clamp(v, lo, hi int64) int64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func queryInt(c *gin.Context, key string, fallback int64) int64 {
	if v, err := strconv.ParseInt(c.Query(key), 10, 64); err == nil {
		return v
	}
	return fallback
}

func queryNullable(c *gin.Context, key string) any {
	if v, ok := c.GetQuery(key); ok {
		return v
	}
	return nil
}

func truncate(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max])
}

func (h *Handler) MediaList(c *gin.Context) error {
	limit := clamp(queryInt(c, "limit", 50), 1, 200)
	offset := queryInt(c, "offset", 0)
	if offset < 0 {
		offset = 0
	}
	rows, err := h.DB.Call(c.Request.Context(), "cms.media_list",
		queryNullable(c, "search"), queryNullable(c, "folder"), c.Query("active") == "true", limit, offset)
	if err != nil {
		return err
	}

	// total_count rides along on every row from the window function; it is
	// the same on each, so it is lifted out rather than repeated.
	var total int64
	if len(rows) > 0 {
		total = toInt64(rows[0]["total_count"])
	}
	for _, row := range rows {
		delete(row, "total_count")
	}
	apierr.OK(c, gin.H{"items": rows, "total": total, "limit": limit, "offset": offset})
	return nil
}

func (h *Handler) MediaGet(c *gin.Context) error {
	id, err := pathID(c, "id")
	if err != nil {
		return err
	}
	ctx := c.Request.Context()
	asset, err := h.DB.One(ctx, "cms.media_get", id)
	if err != nil {
		return err
	}
	// Shown next to the delete button, so the refusal is explained before it
	// happens rather than after.
	usage, err := h.DB.Call(ctx, "cms.media_usage", id)
	if err != nil {
		return err
	}
	apierr.OK(c, gin.H{"asset": asset, "usage": usage})
	return nil
}

func (h *Handler) MediaUpdate(c *gin.Context) error {
	id, err := pathID(c, "id")
	if err != nil {
		return err
	}
	body, err := readJSON(c)
	if err != nil {
		return err
	}
	r := cms.NewReader(body)
	args := []any{
		id,
		r.String("alt_text", cms.StrOpts{Max: 500}),
		cms.Nullable(r.String("title", cms.StrOpts{Max: 200})),
		r.String("folder", cms.StrOpts{Max: 60}),
	}
	if err := r.Done(); err != nil {
		return err
	}
	row, err := h.DB.One(c.Request.Context(), "cms.media_update", args...)
	if err != nil {
		return err
	}
	apierr.OK(c, row)
	return nil
}

func (h *Handler) MediaDelete(c *gin.Context) error {
	id, err := pathID(c, "id")
	if err != nil {
		return err
	}
	// The row goes; the file on disk stays. Deleting bytes that a cached
	// page, a CDN or an external site may still reference is not reversible,
	// and an orphaned file costs a few kilobytes.
	row, err := h.DB.One(c.Request.Context(), "cms.media_delete", id)
	if err != nil {
		return err
	}
	deleted, _ := db.Scalar(row).(bool)
	apierr.OK(c, gin.H{"deleted": deleted})
	return nil
}

func (h *Handler) MediaUpload(c *gin.Context) error {
	user := auth.UserFrom(c)

	if !strings.Contains(c.GetHeader("Content-Type"), "multipart/form-data") {
		return apierr.Validation(nil, "Send the file as multipart/form-data.")
	}
	// Bound the whole request, not just the file part.
	c.Request.Body = httpMaxBytes(c, h.Cfg.MediaMaxBytes+1<<20)

	fh, err := c.FormFile("file")
	if err != nil {
		return apierr.Validation(map[string]string{"file": "No file was attached."}, "")
	}

	stored, err := h.Media.Store(fh)
	if err != nil {
		return err
	}

	folder := c.PostForm("folder")
	if folder == "" {
		folder = "general"
	}

	var uploadedBy any
	if user != nil && user.ID > 0 {
		uploadedBy = user.ID // the bypass identity is not a database row
	}

	row, err := h.DB.One(c.Request.Context(), "cms.media_create",
		stored.URL,
		// The browser's name, kept as a label only. Never a path.
		truncate(fh.Filename, 255),
		stored.MimeType,
		truncate(c.PostForm("alt_text"), 500),
		cms.Nullable(truncate(c.PostForm("title"), 200)),
		stored.ByteSize,
		nil, nil,
		truncate(folder, 60),
		uploadedBy,
	)
	if err != nil {
		// The row is the record of truth; a file with no row is litter
		// nobody can see or delete from the dashboard.
		h.Media.Discard(stored.AbsolutePath)
		return err
	}
	apierr.OK(c, row, 201)
	return nil
}
