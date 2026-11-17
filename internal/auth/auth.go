// Package auth owns passwords, sessions and the guards the routes sit behind.
//
// Everything here is wire-compatible with what the Next.js app used to do, so
// existing password hashes and live sessions keep working across the switch:
// scrypt(N=16384, r=8, p=1, 64 bytes) with the salt's text as the salt bytes,
// and an HMAC-SHA256 of the cookie token as the stored session hash.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/scrypt"

	"cms-backend/internal/apierr"
	"cms-backend/internal/config"
	"cms-backend/internal/db"
)

const CookieName = "sbi_cms_session"

type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email,omitempty"`
	FullName string `json:"fullName"`
	Role     string `json:"role"`
}

// bypassUser is the stand-in identity for the development bypass. Not a
// database row, and deliberately labelled.
var bypassUser = User{ID: 0, Username: "00000", FullName: "Development bypass", Role: "admin"}

type Service struct {
	cfg *config.Config
	db  *db.DB
}

func NewService(cfg *config.Config, d *db.DB) *Service { return &Service{cfg: cfg, db: d} }

/* --- passwords ------------------------------------------------------------ */

func hashPassword(password, salt string) (string, error) {
	key, err := scrypt.Key([]byte(password), []byte(salt), 16384, 8, 1, 64)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(key), nil
}

// VerifyPassword compares in constant time.
func VerifyPassword(password, expectedHash, salt string) bool {
	got, err := hashPassword(password, salt)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(expectedHash)) == 1
}

/* --- sessions ------------------------------------------------------------- */

// tokenHash is an HMAC, not a bare SHA: the server-side secret is a pepper, so
// a dump of admin_sessions cannot be turned into a working cookie.
func (s *Service) tokenHash(token string) string {
	m := hmac.New(sha256.New, []byte(s.cfg.SessionSecret))
	m.Write([]byte(token))
	return hex.EncodeToString(m.Sum(nil))
}

// CreateSession issues a session and sets the cookie.
func (s *Service) CreateSession(c *gin.Context, userID int64) error {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return err
	}
	token := hex.EncodeToString(raw)
	expires := time.Now().Add(s.cfg.SessionTTL)

	ua := c.GetHeader("User-Agent")
	if len(ua) > 500 {
		ua = ua[:500]
	}

	// Only the first hop of x-forwarded-for is meaningful; behind the trusted
	// Next.js proxy it is the real address.
	ip := ClientIP(c)

	if _, err := s.db.One(c.Request.Context(), "cms.session_create",
		userID, s.tokenHash(token), expires, nullable(ua), nullable(ip)); err != nil {
		return err
	}

	http.SetCookie(c.Writer, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   s.cfg.Production,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func (s *Service) DestroySession(c *gin.Context) {
	if token, err := c.Cookie(CookieName); err == nil && token != "" {
		if _, err := s.db.Call(c.Request.Context(), "cms.session_revoke", s.tokenHash(token)); err != nil {
			// Best effort: the cookie is cleared either way.
			log.Printf("[auth] session revoke failed: %v", err)
		}
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name: CookieName, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: s.cfg.Production, SameSite: http.SameSiteLaxMode,
	})
}

// Resolve returns the signed-in user, or nil.
func (s *Service) Resolve(ctx context.Context, c *gin.Context) (*User, error) {
	if s.cfg.DevBypassAuth {
		u := bypassUser
		return &u, nil
	}
	token, err := c.Cookie(CookieName)
	if err != nil || token == "" {
		return nil, nil
	}
	row, err := s.db.One(ctx, "cms.session_resolve", s.tokenHash(token))
	if err != nil || row == nil {
		return nil, err
	}
	id, _ := strconv.ParseInt(fmt.Sprint(row["user_id"]), 10, 64)
	u := &User{ID: id, FullName: str(row["full_name"]), Username: str(row["username"]), Email: str(row["email"]), Role: "editor"}
	if str(row["role"]) == "admin" {
		u.Role = "admin"
	}
	return u, nil
}

func (s *Service) Bypassed() bool { return s.cfg.DevBypassAuth }

const userKey = "cms.user"

// RequireUser is the guard for every admin route.
func (s *Service) RequireUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		u, err := s.Resolve(c.Request.Context(), c)
		if err != nil {
			apierr.Fail(c, err)
			return
		}
		if u == nil {
			apierr.Fail(c, apierr.Unauthorized(""))
			return
		}
		c.Set(userKey, u)
		c.Next()
	}
}

func (s *Service) RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		u := UserFrom(c)
		if u == nil || u.Role != "admin" {
			apierr.Fail(c, apierr.Forbidden("Administrator access required."))
			return
		}
		c.Next()
	}
}

// RequirePublicToken guards the two routes the marketing site reads.
func (s *Service) RequirePublicToken() gin.HandlerFunc {
	return func(c *gin.Context) {
		expected := s.cfg.PublicAPIToken
		if expected == "" {
			// Refusing beats serving unauthenticated.
			apierr.Fail(c, apierr.Unauthorized("CMS_PUBLIC_API_TOKEN is not configured on this server."))
			return
		}
		header := c.GetHeader("Authorization")
		presented := ""
		if strings.HasPrefix(header, "Bearer ") {
			presented = strings.TrimSpace(header[7:])
		}
		if subtle.ConstantTimeCompare([]byte(presented), []byte(expected)) != 1 {
			apierr.Fail(c, apierr.Unauthorized("Invalid or missing bearer token."))
			return
		}
		c.Next()
	}
}

func UserFrom(c *gin.Context) *User {
	if v, ok := c.Get(userKey); ok {
		if u, ok := v.(*User); ok {
			return u
		}
	}
	return nil
}

/* --- login throttle ------------------------------------------------------- */

const (
	throttleWindow = 15 * time.Minute
	maxAttempts    = 10
)

type attempt struct {
	count   int
	resetAt time.Time
}

// Throttle is in-process and therefore per-instance. That is a real limit and
// it is stated rather than papered over: a shared counter needs a store this
// project does not have, and the alternative is no limit at all.
type Throttle struct {
	mu sync.Mutex
	m  map[string]*attempt
}

func NewThrottle() *Throttle { return &Throttle{m: map[string]*attempt{}} }

// Hit records an attempt and reports whether it is within the allowance.
func (t *Throttle) Hit(key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	// Opportunistic sweep so the map cannot grow without bound.
	if len(t.m) > 10000 {
		for k, a := range t.m {
			if a.resetAt.Before(now) {
				delete(t.m, k)
			}
		}
	}
	a, ok := t.m[key]
	if !ok || a.resetAt.Before(now) {
		t.m[key] = &attempt{count: 1, resetAt: now.Add(throttleWindow)}
		return true
	}
	a.count++
	return a.count <= maxAttempts
}

func (t *Throttle) Clear(key string) {
	t.mu.Lock()
	delete(t.m, key)
	t.mu.Unlock()
}

// ClientIP is the first hop of X-Forwarded-For (set by the Next.js proxy), or
// the socket address when the service is called directly.
func ClientIP(c *gin.Context) string {
	if xff := c.GetHeader("X-Forwarded-For"); xff != "" {
		if first, _, _ := strings.Cut(xff, ","); strings.TrimSpace(first) != "" {
			return strings.TrimSpace(first)
		}
	}
	return c.ClientIP()
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func str(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
