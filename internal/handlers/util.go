package handlers

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
)

func httpMaxBytes(c *gin.Context, n int64) io.ReadCloser {
	return http.MaxBytesReader(c.Writer, c.Request.Body, n)
}
