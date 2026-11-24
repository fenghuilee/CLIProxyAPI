package main

import (
	"strings"
	"testing"
	"time"
)

func TestMySQLConfigDSN(t *testing.T) {
	cfg := Config{
		Host:            "127.0.0.1",
		Port:            3306,
		Username:        "user@name",
		Password:        "secret123!",
		Database:        "cliproxyapi",
		Charset:         "utf8mb4",
		Loc:             "Local",
		ParseTime:       true,
		AutoMigrate:     true,
		MaxOpenConns:    20,
		MaxIdleConns:    10,
		ConnMaxLifetime: 30 * time.Minute,
		ConnMaxIdleTime: 10 * time.Minute,
	}

	dsn := cfg.DSN()
	if !strings.Contains(dsn, "user%40name:secret123!@tcp(127.0.0.1:3306)/cliproxyapi") {
		t.Errorf("unexpected DSN: %s", dsn)
	}
	if !strings.Contains(dsn, "charset=utf8mb4") || !strings.Contains(dsn, "parseTime=true") {
		t.Errorf("missing query parameters in DSN: %s", dsn)
	}

	redacted := cfg.RedactedDSN()
	if strings.Contains(redacted, "secret123!") {
		t.Errorf("password was not masked in RedactedDSN: %s", redacted)
	}
	if !strings.Contains(redacted, ":******@") {
		t.Errorf("expected :******@ mask in RedactedDSN: %s", redacted)
	}
}
