package domain

import "time"

// Config holds the fields for a configuration record.
//
// ID is the caller-supplied identifier for the record (see the /configs/:id
// endpoint documentation in the README for the exact semantics). CreatedAt
// and UpdatedAt are populated by the database-backed repository and are
// omitted from JSON output when unset (e.g. for the in-memory repository).
type Config struct {
	ID        string     `json:"id"`
	Host      string     `json:"host"`
	Port      int        `json:"port"`
	AppName   string     `json:"app_name"`
	LogLevel  string     `json:"log_level"`
	CreatedAt *time.Time `json:"created_at,omitempty"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

// Validate performs basic structural validation on a Config prior to
// persistence. It intentionally stays minimal: the goal is to reject
// obviously malformed input, not to fully validate business rules.
func (c *Config) Validate() error {
	if c.ID == "" {
		return errRequired("id")
	}
	if c.Host == "" {
		return errRequired("host")
	}
	if c.Port <= 0 || c.Port > 65535 {
		return errInvalidPort
	}
	if c.AppName == "" {
		return errRequired("app_name")
	}
	if c.LogLevel == "" {
		c.LogLevel = "INFO"
	}
	return nil
}
