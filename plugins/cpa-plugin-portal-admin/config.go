package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// PluginConfig represents the plugin's normalized configuration properties.
type PluginConfig struct {
	DBHost           string `yaml:"db_host" json:"db_host"`
	DBPort           int    `yaml:"db_port" json:"db_port"`
	DBUser           string `yaml:"db_user" json:"db_user"`
	DBPassword       string `yaml:"db_password" json:"db_password"`
	DBName           string `yaml:"db_name" json:"db_name"`
	DBCharset        string `yaml:"db_charset" json:"db_charset"`
	DSN              string `yaml:"dsn" json:"dsn"`
	CPABaseURL       string `yaml:"cpa_base_url" json:"cpa_base_url"`
	CPAManagementKey string `yaml:"cpa_management_key" json:"cpa_management_key"`
}

type rawConfigInput struct {
	DBHost           string `yaml:"db_host" json:"db_host"`
	Host             string `yaml:"host" json:"host"`
	DBPort           int    `yaml:"db_port" json:"db_port"`
	Port             int    `yaml:"port" json:"port"`
	DBUser           string `yaml:"db_user" json:"db_user"`
	Username         string `yaml:"username" json:"username"`
	User             string `yaml:"user" json:"user"`
	DBPassword       string `yaml:"db_password" json:"db_password"`
	Password         string `yaml:"password" json:"password"`
	DBName           string `yaml:"db_name" json:"db_name"`
	Database         string `yaml:"database" json:"database"`
	DBCharset        string `yaml:"db_charset" json:"db_charset"`
	Charset          string `yaml:"charset" json:"charset"`
	DSN              string `yaml:"dsn" json:"dsn"`
	CPABaseURL       string `yaml:"cpa_base_url" json:"cpa_base_url"`
	CPAManagementKey string `yaml:"cpa_management_key" json:"cpa_management_key"`
}

func defaultPluginConfig() PluginConfig {
	return PluginConfig{
		DBHost:     "127.0.0.1",
		DBPort:     3306,
		DBUser:     "root",
		DBPassword: "",
		DBName:     "cliproxyapi",
		DBCharset:  "utf8mb4",
		CPABaseURL: "http://127.0.0.1:8317",
	}
}

func parsePluginConfig(yamlBytes []byte) PluginConfig {
	cfg := defaultPluginConfig()
	if len(yamlBytes) > 0 {
		var raw rawConfigInput
		if err := yaml.Unmarshal(yamlBytes, &raw); err == nil {
			if strings.TrimSpace(raw.DBHost) != "" {
				cfg.DBHost = strings.TrimSpace(raw.DBHost)
			} else if strings.TrimSpace(raw.Host) != "" {
				cfg.DBHost = strings.TrimSpace(raw.Host)
			}

			if raw.DBPort > 0 {
				cfg.DBPort = raw.DBPort
			} else if raw.Port > 0 {
				cfg.DBPort = raw.Port
			}

			if strings.TrimSpace(raw.DBUser) != "" {
				cfg.DBUser = strings.TrimSpace(raw.DBUser)
			} else if strings.TrimSpace(raw.Username) != "" {
				cfg.DBUser = strings.TrimSpace(raw.Username)
			} else if strings.TrimSpace(raw.User) != "" {
				cfg.DBUser = strings.TrimSpace(raw.User)
			}

			if raw.DBPassword != "" {
				cfg.DBPassword = raw.DBPassword
			} else if raw.Password != "" {
				cfg.DBPassword = raw.Password
			}

			if strings.TrimSpace(raw.DBName) != "" {
				cfg.DBName = strings.TrimSpace(raw.DBName)
			} else if strings.TrimSpace(raw.Database) != "" {
				cfg.DBName = strings.TrimSpace(raw.Database)
			}

			if strings.TrimSpace(raw.DBCharset) != "" {
				cfg.DBCharset = strings.TrimSpace(raw.DBCharset)
			} else if strings.TrimSpace(raw.Charset) != "" {
				cfg.DBCharset = strings.TrimSpace(raw.Charset)
			}

			if strings.TrimSpace(raw.DSN) != "" {
				cfg.DSN = strings.TrimSpace(raw.DSN)
			}
			if strings.TrimSpace(raw.CPABaseURL) != "" {
				cfg.CPABaseURL = strings.TrimSpace(raw.CPABaseURL)
			}
			if strings.TrimSpace(raw.CPAManagementKey) != "" {
				cfg.CPAManagementKey = strings.TrimSpace(raw.CPAManagementKey)
			}
		}
	}

	// Environment variable overrides
	if v := os.Getenv("PORTAL_DB_DSN"); v != "" {
		cfg.DSN = v
	} else if v := os.Getenv("MYSQL_DSN"); v != "" {
		cfg.DSN = v
	}

	if v := os.Getenv("PORTAL_DB_HOST"); v != "" {
		cfg.DBHost = v
	} else if v := os.Getenv("MYSQL_HOST"); v != "" {
		cfg.DBHost = v
	}

	if v := os.Getenv("PORTAL_DB_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 {
			cfg.DBPort = p
		}
	} else if v := os.Getenv("MYSQL_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 {
			cfg.DBPort = p
		}
	}

	if v := os.Getenv("PORTAL_DB_USER"); v != "" {
		cfg.DBUser = v
	} else if v := os.Getenv("MYSQL_USER"); v != "" {
		cfg.DBUser = v
	}

	if v := os.Getenv("PORTAL_DB_PASSWORD"); v != "" {
		cfg.DBPassword = v
	} else if v := os.Getenv("MYSQL_PASSWORD"); v != "" {
		cfg.DBPassword = v
	}

	if v := os.Getenv("PORTAL_DB_NAME"); v != "" {
		cfg.DBName = v
	} else if v := os.Getenv("MYSQL_DATABASE"); v != "" {
		cfg.DBName = v
	}

	if v := os.Getenv("CPA_BASE_URL"); v != "" {
		cfg.CPABaseURL = v
	}
	if v := os.Getenv("MANAGEMENT_PASSWORD"); v != "" {
		cfg.CPAManagementKey = v
	}

	return cfg
}

func (c PluginConfig) BuildDSN() string {
	if strings.TrimSpace(c.DSN) != "" {
		return strings.TrimSpace(c.DSN)
	}
	charset := c.DBCharset
	if charset == "" {
		charset = "utf8mb4"
	}
	port := c.DBPort
	if port <= 0 {
		port = 3306
	}
	host := c.DBHost
	if host == "" {
		host = "127.0.0.1"
	}
	dbName := c.DBName
	if dbName == "" {
		dbName = "cliproxyapi"
	}
	user := c.DBUser
	if user == "" {
		user = "root"
	}

	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=True&loc=Local",
		user, c.DBPassword, host, port, dbName, charset)
}
