package main

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Config defines the MySQL database connection and pool parameters.
type Config struct {
	Enabled         bool          `json:"enabled" yaml:"enabled"`
	Priority        int           `json:"priority" yaml:"priority"`
	Host            string        `json:"host" yaml:"host"`
	Port            int           `json:"port" yaml:"port"`
	Username        string        `json:"username" yaml:"username"`
	Password        string        `json:"password" yaml:"password"`
	Database        string        `json:"database" yaml:"database"`
	Charset         string        `json:"charset" yaml:"charset"`
	Loc             string        `json:"loc" yaml:"loc"`
	ParseTime       bool          `json:"parse-time" yaml:"parse-time"`
	AutoMigrate     bool          `json:"auto-migrate" yaml:"auto-migrate"`
	MaxOpenConns    int           `json:"max-open-conns" yaml:"max-open-conns"`
	MaxIdleConns    int           `json:"max-idle-conns" yaml:"max-idle-conns"`
	ConnMaxLifetime time.Duration `json:"conn-max-lifetime" yaml:"conn-max-lifetime"`
	ConnMaxIdleTime time.Duration `json:"conn-max-idle-time" yaml:"conn-max-idle-time"`
}

// DefaultConfig returns safe and optimized production defaults for MySQL.
func DefaultConfig() Config {
	return Config{
		Enabled:         true,
		Host:            "127.0.0.1",
		Port:            3306,
		Username:        "root",
		Database:        "cliproxyapi",
		Charset:         "utf8mb4",
		Loc:             "Local",
		ParseTime:       true,
		AutoMigrate:     false,
		MaxOpenConns:    25,
		MaxIdleConns:    10,
		ConnMaxLifetime: 30 * time.Minute,
		ConnMaxIdleTime: 10 * time.Minute,
	}
}

// DSN builds a standard Go MySQL Driver DSN string from config.
func (c Config) DSN() string {
	charset := c.Charset
	if charset == "" {
		charset = "utf8mb4"
	}
	loc := c.Loc
	if loc == "" {
		loc = "Local"
	}
	port := c.Port
	if port <= 0 {
		port = 3306
	}
	host := c.Host
	if host == "" {
		host = "127.0.0.1"
	}

	auth := url.QueryEscape(c.Username)
	if c.Password != "" {
		auth = fmt.Sprintf("%s:%s", auth, c.Password)
	}

	params := url.Values{}
	params.Set("charset", charset)
	params.Set("parseTime", fmt.Sprintf("%t", c.ParseTime))
	params.Set("loc", loc)
	params.Set("multiStatements", "true")

	return fmt.Sprintf("%s@tcp(%s:%d)/%s?%s",
		auth,
		host,
		port,
		c.Database,
		params.Encode(),
	)
}

// RedactedDSN returns the DSN with password obscured for logging.
func (c Config) RedactedDSN() string {
	dsn := c.DSN()
	if c.Password != "" {
		dsn = strings.Replace(dsn, ":"+c.Password+"@", ":******@", 1)
	}
	return dsn
}
