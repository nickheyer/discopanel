package db

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/go-gormigrate/gormigrate/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func allModels() []any {
	return []any{
		&Server{},
		&ServerConfig{},
		&Mod{},
		&IndexedModpack{},
		&IndexedModpackFile{},
		&ModpackFavorite{},
		&ProxyConfig{},
		&ProxyListener{},
		&User{},
		&Role{},
		&UserRole{},
		&Session{},
		&APIToken{},
		&RegistrationInvite{},
		&ScheduledTask{},
		&TaskExecution{},
		&ModuleTemplate{},
		&Module{},
		&SystemSetting{},
	}
}

func (s *Store) Migrate() error {
	if err := s.backupDB(); err != nil {
		return fmt.Errorf("pre-migration backup failed: %w", err)
	}

	// Create all tables/columns
	if err := s.db.AutoMigrate(allModels()...); err != nil {
		return fmt.Errorf("schema migration failed: %w", err)
	}

	m := gormigrate.New(s.db, &gormigrate.Options{
		TableName:                 "migrations",
		IDColumnName:              "id",
		IDColumnSize:              200,
		UseTransaction:            true,
		ValidateUnknownMigrations: false,
	}, migrations())

	if err := m.Migrate(); err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}

	if err := seeds(s); err != nil {
		return fmt.Errorf("seed failed: %w", err)
	}

	log.Println("[migrate] Migration complete")
	return nil
}

func seeds(s *Store) error {
	for _, seed := range []func() error{
		s.SeedSystemRoles,
		s.SeedGlobalSettings,
	} {
		if err := seed(); err != nil {
			return err
		}
	}
	return nil
}

func migrations() []*gormigrate.Migration {
	return []*gormigrate.Migration{
		{
			ID:      "20260315_001_merge_webhooks_into_tasks",
			Migrate: migrateWebhooksToTasks,
			Rollback: func(tx *gorm.DB) error {
				// One-way migration; rollback is not supported.
				return nil
			},
		},
		{
			ID: "20260306_001_retry_backfill_user_roles",
			Migrate: func(tx *gorm.DB) error {
				// Find users that have no entry in user_roles
				var usersWithoutRoles []User
				if err := tx.Where("id NOT IN (SELECT DISTINCT user_id FROM user_roles)").
					Order("created_at ASC").
					Find(&usersWithoutRoles).Error; err != nil {
					return err
				}

				if len(usersWithoutRoles) == 0 {
					return nil
				}

				var adminCount int64
				tx.Model(&UserRole{}).Where("role_name = ?", "admin").Count(&adminCount)

				for i, user := range usersWithoutRoles {
					roleName := "user"
					if i == 0 && adminCount == 0 {
						roleName = "admin"
					}
					ur := UserRole{
						ID:       user.ID + "-" + roleName,
						UserID:   user.ID,
						RoleName: roleName,
						Source:   "migration",
					}
					if err := tx.Create(&ur).Error; err != nil {
						return fmt.Errorf("failed to assign role %s to user %s: %w", roleName, user.Username, err)
					}
					log.Printf("[migrate] Assigned role '%s' to user '%s'", roleName, user.Username)
				}

				return nil
			},
			Rollback: func(tx *gorm.DB) error {
				return tx.Where("source = ?", "migration").Delete(&UserRole{}).Error
			},
		},
	}
}

// migrateWebhooksToTasks copies rows from the legacy `webhooks` table into
// `scheduled_tasks` (one task per webhook event subscription) and drops the
// legacy table. Idempotent: skips silently if the legacy table is absent.
func migrateWebhooksToTasks(tx *gorm.DB) error {
	// Detect legacy table — sqlite-specific check, fine since we only ship sqlite.
	var tableCount int64
	if err := tx.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='webhooks'").Scan(&tableCount).Error; err != nil {
		return fmt.Errorf("check webhooks table: %w", err)
	}
	if tableCount == 0 {
		return nil
	}

	type legacyWebhook struct {
		ID              string
		ServerID        string
		Name            string
		URL             string
		Secret          string
		Events          string // JSON array
		Enabled         bool
		Format          string
		MaxRetries      int
		RetryDelayMs    int
		TimeoutMs       int
		PayloadTemplate string
		Headers         string // JSON object
		CreatedAt       time.Time
		UpdatedAt       time.Time
	}

	var rows []legacyWebhook
	if err := tx.Raw(`SELECT id, server_id, name, url, secret, events, enabled, format,
		max_retries, retry_delay_ms, timeout_ms, payload_template, headers, created_at, updated_at
		FROM webhooks`).Scan(&rows).Error; err != nil {
		return fmt.Errorf("read webhooks: %w", err)
	}

	eventToTrigger := map[string]TaskEventTrigger{
		"server_start":   TaskEventServerStart,
		"server_stop":    TaskEventServerStop,
		"server_restart": TaskEventServerRestart,
	}

	for _, w := range rows {
		var events []string
		if err := json.Unmarshal([]byte(w.Events), &events); err != nil {
			log.Printf("[migrate] webhook %s: skipping (invalid events JSON: %v)", w.ID, err)
			continue
		}
		var headers map[string]string
		if w.Headers != "" {
			_ = json.Unmarshal([]byte(w.Headers), &headers)
		}

		cfgBytes, err := json.Marshal(map[string]any{
			"url":              w.URL,
			"secret":           w.Secret,
			"format":           w.Format,
			"payload_template": w.PayloadTemplate,
			"headers":          headers,
			"max_retries":      w.MaxRetries,
			"retry_delay_ms":   w.RetryDelayMs,
			"timeout_ms":       w.TimeoutMs,
		})
		if err != nil {
			return fmt.Errorf("marshal webhook config %s: %w", w.ID, err)
		}

		status := TaskStatusEnabled
		if !w.Enabled {
			status = TaskStatusDisabled
		}

		triggers := make([]TaskEventTrigger, 0, len(events))
		for _, ev := range events {
			if t, ok := eventToTrigger[ev]; ok {
				triggers = append(triggers, t)
			}
		}
		if len(triggers) == 0 {
			log.Printf("[migrate] webhook %s (%s): no recognised events, skipping", w.ID, w.Name)
			continue
		}

		task := ScheduledTask{
			ID:            uuid.New().String(),
			ServerID:      w.ServerID,
			Name:          w.Name,
			Description:   "Migrated from webhooks",
			TaskType:      TaskTypeWebhook,
			Status:        status,
			Schedule:      ScheduleTypeEvent,
			EventTriggers: triggers,
			Config:        string(cfgBytes),
			Timeout:       60,
			RequireOnline: false,
			CreatedAt:     w.CreatedAt,
			UpdatedAt:     w.UpdatedAt,
		}
		if err := tx.Create(&task).Error; err != nil {
			return fmt.Errorf("create migrated task for webhook %s: %w", w.ID, err)
		}
		log.Printf("[migrate] webhook %s (%s): migrated with %d event(s)", w.ID, w.Name, len(triggers))
	}

	if err := tx.Exec("DROP TABLE webhooks").Error; err != nil {
		return fmt.Errorf("drop webhooks table: %w", err)
	}
	log.Printf("[migrate] webhooks table dropped after migrating %d row(s)", len(rows))
	return nil
}

func (s *Store) backupDB() error {
	if s.cfg.Database.Path == "" || s.cfg.Database.Path == ":memory:" {
		return nil
	}

	var count int
	row := s.db.Raw("SELECT count(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Row()
	if err := row.Scan(&count); err != nil || count == 0 {
		return nil
	}

	backupPath := s.cfg.Database.Path + ".pre-migrate.bak"
	os.Remove(backupPath)
	if err := s.db.Exec("VACUUM INTO ?", backupPath).Error; err != nil {
		return fmt.Errorf("VACUUM INTO %s: %w", backupPath, err)
	}

	log.Printf("[migrate] Database backed up to %s", backupPath)
	return nil
}
