package db

import (
	"fmt"
	"log"
	"os"
	"regexp"

	"github.com/go-gormigrate/gormigrate/v2"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	"github.com/nickheyer/discopanel/pkg/runtimespec"
	"gorm.io/gorm"
)

func (s *Store) Migrate() error {
	pending, err := s.pendingMigrations()
	if err != nil {
		return fmt.Errorf("failed to check pending migrations: %w", err)
	}
	if pending {
		if err := s.backupDB(); err != nil {
			return fmt.Errorf("pre-migration backup failed: %w", err)
		}
	}

	// Create all tables/columns
	if err := s.db.AutoMigrate(v1.AllModels()...); err != nil {
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
			ID: "20260306_001_retry_backfill_user_roles",
			Migrate: func(tx *gorm.DB) error {
				// Find users that have no entry in user_roles
				var usersWithoutRoles []*v1.User
				if err := tx.Where("id NOT IN (SELECT DISTINCT user_id FROM user_roles)").
					Order("created_at ASC").
					Find(&usersWithoutRoles).Error; err != nil {
					return err
				}

				if len(usersWithoutRoles) == 0 {
					return nil
				}

				var adminCount int64
				tx.Model(&v1.UserRole{}).Where("role_name = ?", "admin").Count(&adminCount)

				for i, user := range usersWithoutRoles {
					roleName := "user"
					if i == 0 && adminCount == 0 {
						roleName = "admin"
					}
					ur := &v1.UserRole{
						Id:       user.Id + "-" + roleName,
						UserId:   user.Id,
						RoleName: roleName,
						Source:   "migration",
					}
					if err := tx.Create(ur).Error; err != nil {
						return fmt.Errorf("failed to assign role %s to user %s: %w", roleName, user.Username, err)
					}
					log.Printf("[migrate] Assigned role '%s' to user '%s'", roleName, user.Username)
				}

				return nil
			},
			Rollback: func(tx *gorm.DB) error {
				return tx.Where("source = ?", "migration").Delete(&v1.UserRole{}).Error
			},
		},
		{
			// Clears stale itzg image tags for re-derivation on start
			ID: "20260701_001_reset_itzg_image_tags",
			Migrate: func(tx *gorm.DB) error {
				result := tx.Model(&v1.Server{}).Where("docker_image != ''").Update("docker_image", "")
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected > 0 {
					log.Printf("[migrate] Cleared %d legacy itzg image tag(s); runtime images are now derived from the MC version", result.RowsAffected)
				}
				return nil
			},
			Rollback: func(tx *gorm.DB) error {
				return nil
			},
		},
		{
			// Adopts old server_configs rows under the new table name
			ID: "20260708_001_rename_server_configs_to_server_properties",
			Migrate: func(tx *gorm.DB) error {
				if !tx.Migrator().HasTable("server_configs") {
					return nil
				}
				if tx.Migrator().HasTable("server_properties") {
					var count int64
					if err := tx.Table("server_properties").Count(&count).Error; err != nil {
						return err
					}
					if count > 0 {
						log.Println("[migrate] server_properties already has rows, keeping both tables")
						return nil
					}
					if err := tx.Migrator().DropTable("server_properties"); err != nil {
						return err
					}
				}
				if err := tx.Migrator().RenameTable("server_configs", "server_properties"); err != nil {
					return err
				}
				if err := tx.Exec("DROP INDEX IF EXISTS idx_server_configs_server_id").Error; err != nil {
					return err
				}
				log.Println("[migrate] Renamed server_configs to server_properties")
				return tx.AutoMigrate(&v1.ServerProperties{})
			},
			Rollback: func(tx *gorm.DB) error {
				return tx.Migrator().RenameTable("server_properties", "server_configs")
			},
		},
		{
			// Stored casbin policies still grant the old server_config resource
			ID: "20260708_002_rename_server_config_rbac_resource",
			Migrate: func(tx *gorm.DB) error {
				if !tx.Migrator().HasTable("casbin_rule") {
					return nil
				}
				return tx.Exec("UPDATE casbin_rule SET v1 = 'server_properties' WHERE v1 = 'server_config'").Error
			},
			Rollback: func(tx *gorm.DB) error {
				if !tx.Migrator().HasTable("casbin_rule") {
					return nil
				}
				return tx.Exec("UPDATE casbin_rule SET v1 = 'server_config' WHERE v1 = 'server_properties'").Error
			},
		},
		{
			// Moves heap sizing from properties onto the server row
			ID: "20260709_001_backfill_server_heap_sizing",
			Migrate: func(tx *gorm.DB) error {
				hasColumn := func(name string) bool {
					var count int64
					tx.Raw("SELECT count(*) FROM pragma_table_info('server_properties') WHERE name = ?", name).Scan(&count)
					return count > 0
				}

				type propsRow struct {
					ServerID   string
					InitMemory *string
					MaxMemory  *string
					Memory     *string
					AutoMemory *bool
				}
				cols := "server_id, init_memory, max_memory"
				if hasColumn("memory") {
					cols += ", memory"
				}
				if hasColumn("auto_memory") {
					cols += ", auto_memory"
				}
				var props []propsRow
				if err := tx.Table("server_properties").Select(cols).Scan(&props).Error; err != nil {
					return err
				}
				propsByServer := make(map[string]propsRow, len(props))
				for _, p := range props {
					propsByServer[p.ServerID] = p
				}

				strVal := func(s *string) string {
					if s == nil {
						return ""
					}
					return *s
				}

				var servers []*v1.Server
				if err := tx.Where("memory_min = 0 AND memory_max = 0").Find(&servers).Error; err != nil {
					return err
				}
				for _, srv := range servers {
					initMB, maxMB := 0, 0
					p, ok := propsByServer[srv.Id]
					autoMem := ok && p.AutoMemory != nil && *p.AutoMemory
					if ok && !autoMem {
						initMB = runtimespec.ParseMemoryMB(strVal(p.InitMemory))
						maxMB = runtimespec.ParseMemoryMB(strVal(p.MaxMemory))
						if maxMB == 0 {
							maxMB = runtimespec.ParseMemoryMB(strVal(p.Memory))
							if initMB == 0 {
								initMB = maxMB
							}
						}
					}
					memory := int(srv.Memory)
					defInit, defMax := DefaultHeapForMemory(memory)
					if maxMB <= 0 {
						maxMB = defMax
					}
					if initMB <= 0 {
						initMB = defInit
					}
					if memory > 0 && maxMB > memory {
						maxMB = memory
					}
					if initMB > maxMB {
						initMB = maxMB
					}
					if err := tx.Model(&v1.Server{}).Where("id = ?", srv.Id).
						Updates(map[string]any{"memory_min": initMB, "memory_max": maxMB}).Error; err != nil {
						return err
					}
				}
				if len(servers) > 0 {
					log.Printf("[migrate] Backfilled heap sizing for %d server(s)", len(servers))
				}
				return nil
			},
			Rollback: func(tx *gorm.DB) error {
				return nil
			},
		},
		{
			// Ledger actor column became source with structured fields
			ID: "20260711_001_server_actions_actor_to_source",
			Migrate: func(tx *gorm.DB) error {
				var count int64
				tx.Raw("SELECT count(*) FROM pragma_table_info('server_actions') WHERE name = 'actor'").Scan(&count)
				if count == 0 {
					return nil
				}
				if err := tx.Exec("UPDATE server_actions SET source = actor WHERE source = '' OR source IS NULL").Error; err != nil {
					return err
				}
				if err := tx.Exec("ALTER TABLE server_actions DROP COLUMN actor").Error; err != nil {
					return err
				}
				log.Println("[migrate] Renamed server_actions.actor to source")
				return nil
			},
			Rollback: func(tx *gorm.DB) error {
				return nil
			},
		},
		{
			// Enforced properties FK rejects the global settings pseudo row
			ID: "20260712_001_fk_enforcement_cleanup",
			Migrate: func(tx *gorm.DB) error {
				var ddl string
				if err := tx.Raw("SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'server_properties'").Scan(&ddl).Error; err != nil {
					return err
				}
				fkClause := regexp.MustCompile("(?i),\\s*CONSTRAINT\\s+`?\\w+`?\\s+FOREIGN\\s+KEY\\s*\\(`?server_id`?\\)\\s*REFERENCES\\s*`?servers`?\\s*\\(`?id`?\\)(\\s+ON\\s+(DELETE|UPDATE)\\s+\\w+(\\s\\w+)?)*")
				if ddl != "" && fkClause.MatchString(ddl) {
					newDDL := fkClause.ReplaceAllString(ddl, "")
					newDDL = regexp.MustCompile("(?i)^CREATE TABLE\\s+`?server_properties`?").ReplaceAllString(newDDL, "CREATE TABLE `server_properties_fkfree`")
					for _, stmt := range []string{
						newDDL,
						"INSERT INTO server_properties_fkfree SELECT * FROM server_properties",
						"DROP TABLE server_properties",
						"ALTER TABLE server_properties_fkfree RENAME TO server_properties",
						"CREATE INDEX IF NOT EXISTS idx_server_properties_server_id ON server_properties(server_id)",
					} {
						if err := tx.Exec(stmt).Error; err != nil {
							return err
						}
					}
					log.Println("[migrate] Rebuilt server_properties without the servers foreign key")
				}

				removed := int64(0)
				for _, stmt := range []string{
					"DELETE FROM api_tokens WHERE id IN (SELECT token_id FROM modules WHERE token_id != '' AND server_id NOT IN (SELECT id FROM servers))",
					"DELETE FROM modules WHERE server_id NOT IN (SELECT id FROM servers)",
					"DELETE FROM task_executions WHERE server_id NOT IN (SELECT id FROM servers) OR task_id NOT IN (SELECT id FROM scheduled_tasks)",
					"DELETE FROM scheduled_tasks WHERE server_id NOT IN (SELECT id FROM servers)",
					"DELETE FROM mods WHERE server_id NOT IN (SELECT id FROM servers)",
					"DELETE FROM metrics_samples WHERE server_id NOT IN (SELECT id FROM servers)",
					"DELETE FROM server_actions WHERE server_id NOT IN (SELECT id FROM servers)",
					"DELETE FROM finding_dismissals WHERE server_id NOT IN (SELECT id FROM servers)",
					"DELETE FROM server_properties WHERE server_id != '" + GlobalSettingsID + "' AND server_id NOT IN (SELECT id FROM servers)",
				} {
					res := tx.Exec(stmt)
					if res.Error != nil {
						return res.Error
					}
					removed += res.RowsAffected
				}
				if removed > 0 {
					log.Printf("[migrate] Purged %d orphaned server-scoped row(s)", removed)
				}
				return nil
			},
			Rollback: func(tx *gorm.DB) error {
				return nil
			},
		},
		{
			// Module tokens rotate per container create, plaintext never persists
			ID: "20260712_002_drop_module_token_plaintext",
			Migrate: func(tx *gorm.DB) error {
				var count int64
				if err := tx.Raw("SELECT COUNT(*) FROM pragma_table_info('modules') WHERE name = 'token_plaintext'").Scan(&count).Error; err != nil {
					return err
				}
				if count == 0 {
					return nil
				}
				return tx.Exec("ALTER TABLE modules DROP COLUMN token_plaintext").Error
			},
			Rollback: func(tx *gorm.DB) error {
				return nil
			},
		},
		{
			// Existing template rows learn the container port alias
			ID: "20260713_001_container_port_alias",
			Migrate: func(tx *gorm.DB) error {
				for _, stmt := range []string{
					`UPDATE module_templates SET default_env = REPLACE(default_env, '"REMOTE_PORT": "25565"', '"REMOTE_PORT": "{{server.container_port}}"')`,
					`UPDATE module_templates SET default_env = REPLACE(default_env, '{{server.id}}:25565', '{{server.id}}:{{server.container_port}}')`,
				} {
					if err := tx.Exec(stmt).Error; err != nil {
						return err
					}
				}
				return nil
			},
			Rollback: func(tx *gorm.DB) error {
				return nil
			},
		},
		{
			// Webhook vars now follow alias names, color moved into presets
			ID: "20260712_003_webhook_template_vocab",
			Migrate: func(tx *gorm.DB) error {
				colorCond := "{{if .is_server_start}}5763719{{else if .is_server_stop}}15548997{{else if .is_server_restart}}16705372{{else}}5793266{{end}}"
				for _, stmt := range []string{
					"UPDATE scheduled_tasks SET config = REPLACE(config, '{{.server_players}}', '{{.server_players_online}}') WHERE task_type = 'webhook'",
					"UPDATE scheduled_tasks SET config = REPLACE(config, '{{.color}}', '" + colorCond + "') WHERE task_type = 'webhook'",
				} {
					if err := tx.Exec(stmt).Error; err != nil {
						return err
					}
				}
				return nil
			},
			Rollback: func(tx *gorm.DB) error {
				return nil
			},
		},
	}
}

// Reports whether any registered migration has not run yet
func (s *Store) pendingMigrations() (bool, error) {
	if !s.db.Migrator().HasTable("migrations") {
		return true, nil
	}
	var applied []string
	if err := s.db.Table("migrations").Pluck("id", &applied).Error; err != nil {
		return false, err
	}
	appliedSet := make(map[string]bool, len(applied))
	for _, id := range applied {
		appliedSet[id] = true
	}
	for _, m := range migrations() {
		if !appliedSet[m.ID] {
			return true, nil
		}
	}
	return false, nil
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
