package hub

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/whg517/rustdesk-hub/internal/hbbs"
	"github.com/whg517/rustdesk-hub/internal/rustdesk"
)

const rustDeskSettingsID = 1

type RustDeskSettings struct {
	rustdesk.ServerSettings `gorm:"embedded"`
	OnlineEnabled           bool   `json:"onlineEnabled"`
	OnlineServer            string `json:"onlineServer"`
}

type rustDeskSettingsRow struct {
	ID               uint `gorm:"primaryKey;autoIncrement:false"`
	RustDeskSettings `gorm:"embedded"`
}

func (rustDeskSettingsRow) TableName() string { return "rustdesk_settings" }

func (s RustDeskSettings) ClientSetup(publicURL string) (*rustdesk.Setup, error) {
	if s.ServerSettings == (rustdesk.ServerSettings{}) {
		return nil, nil
	}
	return rustdesk.NewSetup(s.ServerSettings, publicURL)
}

func (s RustDeskSettings) OnlineAddress() string {
	if !s.OnlineEnabled {
		return ""
	}
	if s.OnlineServer != "" {
		return s.OnlineServer
	}
	return s.IDServer
}

func (s RustDeskSettings) normalize() (RustDeskSettings, error) {
	s.IDServer = strings.TrimSpace(s.IDServer)
	s.RelayServer = strings.TrimSpace(s.RelayServer)
	s.PublicKey = strings.TrimSpace(s.PublicKey)
	s.OnlineServer = strings.TrimSpace(s.OnlineServer)
	if s.ServerSettings != (rustdesk.ServerSettings{}) {
		profile, err := s.ServerSettings.Normalize()
		if err != nil {
			return RustDeskSettings{}, InputError(err.Error())
		}
		s.ServerSettings = profile
	}
	address := s.OnlineServer
	if s.OnlineEnabled {
		address = s.OnlineAddress()
		if address == "" {
			return RustDeskSettings{}, InputError("启用在线查询时，请填写 ID 服务器或在线查询地址")
		}
	}
	if address != "" {
		if _, err := hbbs.New(address); err != nil {
			return RustDeskSettings{}, InputError("在线查询地址无效。" + err.Error())
		}
	}
	return s, nil
}

func readRustDeskSettings(tx *gorm.DB) (RustDeskSettings, error) {
	var row rustDeskSettingsRow
	err := tx.Take(&row, rustDeskSettingsID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return RustDeskSettings{}, nil
	}
	return row.RustDeskSettings, err
}

func (s *Store) RustDeskSettings(ctx context.Context) (RustDeskSettings, error) {
	return readRustDeskSettings(s.db.WithContext(ctx))
}

func (s *Store) AdminRustDeskSettings(ctx context.Context, credential Credential) (RustDeskSettings, error) {
	var settings RustDeskSettings
	err := s.withAdmin(ctx, credential, func(tx *gorm.DB) error {
		var err error
		settings, err = readRustDeskSettings(tx)
		return err
	})
	return settings, err
}

func (s *Store) SaveRustDeskSettings(ctx context.Context, credential Credential, input RustDeskSettings) (RustDeskSettings, error) {
	var settings RustDeskSettings
	err := s.withAdmin(ctx, credential, func(tx *gorm.DB) error {
		var err error
		settings, err = input.normalize()
		if err != nil {
			return err
		}
		row := rustDeskSettingsRow{ID: rustDeskSettingsID, RustDeskSettings: settings}
		return tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&row).Error
	})
	return settings, err
}

// SeedRustDeskSettings initializes the singleton once, including on fresh installs.
func (s *Store) SeedRustDeskSettings(ctx context.Context, input RustDeskSettings) (bool, error) {
	var inserted bool
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Take(&rustDeskSettingsRow{}, rustDeskSettingsID).Error
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		settings, err := input.normalize()
		if err != nil {
			return err
		}
		row := rustDeskSettingsRow{ID: rustDeskSettingsID, RustDeskSettings: settings}
		inserted, err = insertIfNew(tx, &row, "id")
		return err
	})
	return inserted, err
}
