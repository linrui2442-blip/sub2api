// Package repository provides the Personal Edition persistence adapters.
package repository

import (
	"database/sql"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
)

// InitEnt opens the single-file SQLite database used by Personal Edition.
// PostgreSQL and the upstream dual-runtime migration path are intentionally not
// supported in this edition.
func InitEnt(cfg *config.Config) (*ent.Client, *sql.DB, error) {
	return initPersonalEnt(cfg)
}
