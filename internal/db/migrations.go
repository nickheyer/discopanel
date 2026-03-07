package db

import (
	"fmt"
	"log"
	"os"

	"github.com/go-gormigrate/gormigrate/v2"
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
