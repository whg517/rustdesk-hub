package hub

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	ErrCredentials    = errors.New("invalid credentials")
	ErrSession        = errors.New("invalid session")
	ErrNotFound       = errors.New("not found")
	ErrTagExists      = errors.New("tag already exists")
	ErrOwnerExists    = errors.New("owner already exists")
	ErrUserExists     = errors.New("account already exists")
	ErrUserNotFound   = errors.New("account not found")
	ErrAdminRequired  = errors.New("administrator required")
	ErrCredentialSync = errors.New("device credential sync is disabled")
)

const ownerID uint = 1

type InputError string

func (e InputError) Error() string { return string(e) }

type Source string

const (
	Native Source = "rustdesk"
	Web    Source = "web"
)

type Credential struct {
	Token  string
	Source Source
}

type Store struct {
	db           *gorm.DB
	conn         *sql.DB
	passwordJobs chan struct{}
}

type account struct {
	ID             uint   `gorm:"primaryKey"`
	Name           string `gorm:"not null;uniqueIndex"`
	PasswordHash   string `gorm:"not null"`
	SessionVersion uint64 `gorm:"not null"`
	Disabled       bool   `gorm:"not null;default:false"`
}

type session struct {
	Digest    string    `gorm:"primaryKey"`
	AccountID uint      `gorm:"not null"`
	Source    Source    `gorm:"not null"`
	Version   uint64    `gorm:"not null"`
	ExpiresAt time.Time `gorm:"index;not null"`
	RevokedAt *time.Time
}

type addressBook struct {
	ID        uint   `gorm:"primaryKey"`
	AccountID uint   `gorm:"uniqueIndex;not null"`
	GUID      string `gorm:"uniqueIndex;not null"`
}

type peer struct {
	ID       uint   `gorm:"primaryKey"`
	BookID   uint   `gorm:"uniqueIndex:peer_device;not null"`
	DeviceID string `gorm:"uniqueIndex:peer_device;not null"`
	Alias    string `gorm:"not null"`
	Note     string `gorm:"not null"`
	Tags     []tag  `gorm:"many2many:peer_tags;joinForeignKey:PeerID;joinReferences:TagID;constraint:OnDelete:CASCADE"`
}

type tag struct {
	ID     uint   `gorm:"primaryKey"`
	BookID uint   `gorm:"uniqueIndex:tag_name;not null"`
	Name   string `gorm:"uniqueIndex:tag_name;not null"`
	Color  uint32 `gorm:"not null"`
}

type peerTag struct {
	PeerID uint `gorm:"primaryKey;autoIncrement:false"`
	TagID  uint `gorm:"primaryKey;autoIncrement:false"`
}

func Open(path string) (*Store, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(absolute, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}

	dsn := url.URL{
		Scheme:   "file",
		Path:     absolute,
		RawQuery: "_foreign_keys=on&_busy_timeout=5000&_journal_mode=WAL",
	}
	db, err := gorm.Open(sqlite.Open(dsn.String()), &gorm.Config{
		Logger:         logger.Default.LogMode(logger.Silent),
		TranslateError: true,
	})
	if err != nil {
		return nil, fmt.Errorf("open SQLite: %w", err)
	}
	conn, err := db.DB()
	if err != nil {
		return nil, err
	}
	// One connection serializes in-process writes and keeps authorization and updates in the same transaction.
	conn.SetMaxOpenConns(1)
	if err := db.SetupJoinTable(&peer{}, "Tags", &peerTag{}); err != nil {
		conn.Close()
		return nil, err
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if tx.Migrator().HasTable(&peer{}) {
			if err := normalizeStoredDeviceIDs(tx); err != nil {
				return err
			}
		}
		return tx.AutoMigrate(&account{}, &session{}, &addressBook{}, &tag{}, &peer{}, &peerTag{}, &Diagnostic{}, &rustDeskSettingsRow{})
	}); err != nil {
		conn.Close()
		return nil, fmt.Errorf("migrate SQLite: %w", err)
	}

	return &Store{db: db, conn: conn, passwordJobs: make(chan struct{}, 2)}, nil
}

func normalizeStoredDeviceIDs(tx *gorm.DB) error {
	var duplicate struct{ DeviceID string }
	conflicts := tx.Raw(`
			SELECT replace(device_id, ' ', '') AS device_id
			FROM peers
			GROUP BY book_id, replace(device_id, ' ', '')
			HAVING count(*) > 1
			LIMIT 1
		`).Scan(&duplicate)
	if conflicts.Error != nil {
		return conflicts.Error
	}
	if conflicts.RowsAffected > 0 {
		return fmt.Errorf("设备 ID %q 规范化后重复。原条目已保留，请在升级前的版本中核对并处理重复记录", duplicate.DeviceID)
	}
	return tx.Exec("UPDATE peers SET device_id = replace(device_id, ' ', '') WHERE instr(device_id, ' ') > 0").Error
}

func (s *Store) Close() error { return s.conn.Close() }

func (s *Store) Ping(ctx context.Context) error {
	// Probe the initialized account and book instead of just the SQLite connection handle.
	var ready int
	return s.conn.QueryRowContext(ctx, `
		SELECT 1 FROM accounts
		JOIN address_books ON address_books.account_id = accounts.id
		WHERE accounts.id = ?`, ownerID).Scan(&ready)
}

func owner(tx *gorm.DB) (account, error) {
	var a account
	err := tx.First(&a, ownerID).Error
	return a, err
}

func (s *Store) HasOwner(ctx context.Context) (bool, error) {
	_, err := owner(s.db.WithContext(ctx))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return err == nil, err
}
