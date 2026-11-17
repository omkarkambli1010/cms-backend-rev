// Package apierr holds the one response envelope and the one place that maps
// a failure onto an HTTP status. Handlers return errors; they never build
// error responses by hand, so a new failure mode cannot leak a stack trace.
package apierr

import (
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"
)

type Error struct {
	Status  int
	Code    string
	Message string
	Fields  map[string]string
}

func (e *Error) Error() string { return e.Message }

func Unauthorized(msg string) *Error {
	if msg == "" {
		msg = "Authentication required."
	}
	return &Error{Status: http.StatusUnauthorized, Code: "unauthorized", Message: msg}
}

func Forbidden(msg string) *Error {
	if msg == "" {
		msg = "You do not have permission to do that."
	}
	return &Error{Status: http.StatusForbidden, Code: "forbidden", Message: msg}
}

func NotFound(msg string) *Error {
	if msg == "" {
		msg = "Not found."
	}
	return &Error{Status: http.StatusNotFound, Code: "not_found", Message: msg}
}

func Validation(fields map[string]string, msg string) *Error {
	if msg == "" {
		msg = "Please correct the highlighted fields."
	}
	return &Error{Status: http.StatusUnprocessableEntity, Code: "validation", Message: msg, Fields: fields}
}

// pgStatus lists the SQLSTATEs the stored routines raise deliberately.
var pgStatus = map[string]int{
	"23505": 409, // unique_violation
	"23503": 409, // foreign_key_violation
	"23514": 400, // check_violation
	"23502": 400, // not_null_violation
	"P0002": 404, // no_data_found
	"23001": 409, // restrict_violation
	"22P02": 400, // invalid_text_representation
}

// unreachable are failures meaning "the database is not there", as opposed to
// "the database refused this". An operator fixes the first, an editor the second.
var unreachable = map[string]bool{
	"28P01": true, "28000": true, "3D000": true, "57P03": true, "08006": true, "08001": true,
}

func dbDown(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"ok": false, "error": gin.H{
		"message": "The database is not responding. Nothing can be read or saved until it is.",
		"code":    "db_unreachable",
	}})
}

// Fail writes err as the response. Anything that is not an *Error is treated
// as an internal fault and is not described to the caller.
func Fail(c *gin.Context, err error) {
	var ae *Error
	if errors.As(err, &ae) {
		body := gin.H{"message": ae.Message, "code": ae.Code}
		if ae.Fields != nil {
			body["fields"] = ae.Fields
		}
		c.AbortWithStatusJSON(ae.Status, gin.H{"ok": false, "error": body})
		return
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if unreachable[pgErr.Code] {
			log.Printf("[api] database unreachable: %s %s", pgErr.Code, pgErr.Message)
			dbDown(c)
			return
		}

		status, known := pgStatus[pgErr.Code]
		if !known {
			status = http.StatusInternalServerError
		}
		// A deliberate RAISE carries a message written for an editor, so it
		// is safe to surface. Anything else is an internal fault.
		message := "Something went wrong. Please try again."
		if status < 500 {
			message = pgErr.Message
		} else {
			log.Printf("[api] database error: %s %s", pgErr.Code, pgErr.Message)
		}
		c.AbortWithStatusJSON(status, gin.H{"ok": false, "error": gin.H{"message": message, "code": "pg_" + pgErr.Code}})
		return
	}

	var connErr *pgconn.ConnectError
	if errors.As(err, &connErr) || pgconn.Timeout(err) {
		log.Printf("[api] database unreachable: %v", err)
		dbDown(c)
		return
	}

	log.Printf("[api] unhandled error: %v", err)
	c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"ok": false, "error": gin.H{
		"message": "Unexpected server error.", "code": "internal",
	}})
}

// OK writes the success envelope.
func OK(c *gin.Context, data any, status ...int) {
	code := http.StatusOK
	if len(status) > 0 {
		code = status[0]
	}
	c.JSON(code, gin.H{"ok": true, "data": data})
}
