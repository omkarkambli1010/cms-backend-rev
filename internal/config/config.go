// Package config reads the service's settings from the environment, loading a
// .env file first when one is present. Real environment variables win over the
// file, so a deployment can override anything without editing it.
package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr             string
	Production       bool
	DatabaseURL      string
	PoolMax          int32
	StatementTimeout time.Duration
	SessionSecret    string
	SessionTTL       time.Duration
	PublicAPIToken   string
	MediaDir         string
	MediaPublicPath  string
	MediaMaxBytes    int64
	DevBypassAuth    bool
}

func loadEnvFile(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
			value = value[1 : len(value)-1]
		}
		if _, set := os.LookupEnv(key); !set {
			os.Setenv(key, value)
		}
	}
}

func intEnv(key string, fallback int64) int64 {
	if v, err := strconv.ParseInt(strings.TrimSpace(os.Getenv(key)), 10, 64); err == nil && v > 0 {
		return v
	}
	return fallback
}

// Load builds the configuration and refuses to start when something the
// service cannot run safely without is missing.
func Load() (*Config, error) {
	loadEnvFile(".env")

	c := &Config{
		Addr:             strings.TrimSpace(os.Getenv("CMS_BACKEND_ADDR")),
		Production:       strings.EqualFold(os.Getenv("APP_ENV"), "production"),
		DatabaseURL:      strings.TrimSpace(os.Getenv("DATABASE_URL")),
		PoolMax:          int32(intEnv("DATABASE_POOL_MAX", 10)),
		StatementTimeout: time.Duration(intEnv("DATABASE_STATEMENT_TIMEOUT_MS", 8000)) * time.Millisecond,
		SessionSecret:    strings.TrimSpace(os.Getenv("CMS_SESSION_SECRET")),
		SessionTTL:       time.Duration(intEnv("CMS_SESSION_TTL_HOURS", 12)) * time.Hour,
		PublicAPIToken:   strings.TrimSpace(os.Getenv("CMS_PUBLIC_API_TOKEN")),
		MediaDir:         strings.TrimSpace(os.Getenv("CMS_MEDIA_DIR")),
		MediaPublicPath:  strings.TrimRight(strings.TrimSpace(os.Getenv("CMS_MEDIA_PUBLIC_PATH")), "/"),
		MediaMaxBytes:    intEnv("CMS_MEDIA_MAX_BYTES", 5*1024*1024),
	}

	if c.Addr == "" {
		// Loopback by default: the Next.js server is the only intended caller.
		c.Addr = "127.0.0.1:8080"
	}
	if c.MediaDir == "" {
		c.MediaDir = "../cms-website/public/uploads"
	}
	if c.MediaPublicPath == "" {
		c.MediaPublicPath = "/uploads"
	}
	// A production process ignores the bypass whatever the variable says.
	c.DevBypassAuth = !c.Production && os.Getenv("CMS_DEV_BYPASS_AUTH") == "true"

	var missing []string
	if c.DatabaseURL == "" {
		missing = append(missing, "DATABASE_URL")
	}
	if len(c.SessionSecret) < 32 {
		missing = append(missing, "CMS_SESSION_SECRET (at least 32 characters)")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing configuration: %s", strings.Join(missing, ", "))
	}
	return c, nil
}
