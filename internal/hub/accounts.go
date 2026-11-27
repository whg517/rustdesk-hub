package hub

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	maxPasswordBytes = 256
	saltBytes        = 16
	keyBytes         = 32
	argonPrefix      = "$argon2id$v=19$m=65536,t=3,p=2$"
	// Keep the password verification cost the same for an unknown username.
	dummyPasswordHash = argonPrefix + "AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
)

type User struct {
	ID       uint
	Name     string
	IsAdmin  bool
	Disabled bool
}

type LoginResult struct {
	User      User
	Token     string
	ExpiresAt time.Time
}

func (a account) user() User {
	return User{ID: a.ID, Name: a.Name, IsAdmin: a.ID == ownerID, Disabled: a.Disabled}
}

func validateUsername(name string) error {
	if name == "" || len(name) > 64 || strings.ContainsFunc(name, unicode.IsSpace) || strings.ContainsFunc(name, unicode.IsControl) {
		return InputError("账号名须在 1 至 64 字节以内，且不包含空格或控制字符")
	}
	return nil
}

func validatePassword(password string) error {
	if utf8.RuneCountInString(password) < 12 || len(password) > maxPasswordBytes {
		return InputError("密码至少需要 12 个字符，最多 256 字节")
	}
	return nil
}

func (s *Store) passwordKey(ctx context.Context, password string, salt []byte) ([]byte, error) {
	select {
	case s.passwordJobs <- struct{}{}:
		defer func() { <-s.passwordJobs }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, keyBytes), nil
}

func (s *Store) hashPassword(ctx context.Context, password string) (string, error) {
	var salt [saltBytes]byte
	rand.Read(salt[:])
	key, err := s.passwordKey(ctx, password, salt[:])
	if err != nil {
		return "", err
	}
	return argonPrefix + base64.RawStdEncoding.EncodeToString(salt[:]) + "$" + base64.RawStdEncoding.EncodeToString(key), nil
}

func (s *Store) verifyPassword(ctx context.Context, password, hash string) (bool, error) {
	if password == "" || len(password) > maxPasswordBytes {
		return false, nil
	}
	fields, ok := strings.CutPrefix(hash, argonPrefix)
	saltText, keyText, split := strings.Cut(fields, "$")
	salt, saltErr := base64.RawStdEncoding.DecodeString(saltText)
	want, keyErr := base64.RawStdEncoding.DecodeString(keyText)
	if !ok || !split || saltErr != nil || keyErr != nil || len(salt) != saltBytes || len(want) != keyBytes {
		return false, errors.New("invalid stored password hash")
	}
	have, err := s.passwordKey(ctx, password, salt)
	if err != nil {
		return false, err
	}
	return subtle.ConstantTimeCompare(have, want) == 1, nil
}

func (s *Store) InitOwner(ctx context.Context, name, password string) error {
	if err := validateUsername(name); err != nil {
		return err
	}
	if err := validatePassword(password); err != nil {
		return err
	}
	hash, err := s.hashPassword(ctx, password)
	if err != nil {
		return err
	}
	a := account{ID: ownerID, Name: name, PasswordHash: hash, SessionVersion: 1}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := createAccount(tx, &a)
		if errors.Is(err, ErrUserExists) {
			return ErrOwnerExists
		}
		return err
	})
}

func createAccount(tx *gorm.DB, a *account) error {
	if err := tx.Create(a).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return ErrUserExists
		}
		return err
	}
	return tx.Create(&addressBook{AccountID: a.ID, GUID: rand.Text()}).Error
}

func tokenDigest(token string) (string, error) {
	if len(token) != base64.RawURLEncoding.EncodedLen(keyBytes) {
		return "", ErrSession
	}
	if _, err := base64.RawURLEncoding.Strict().DecodeString(token); err != nil {
		return "", ErrSession
	}
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:]), nil
}

func (s *Store) Login(ctx context.Context, name, password string, source Source) (LoginResult, error) {
	var a account
	err := s.db.WithContext(ctx).Where("name = ?", name).Take(&a).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		a.PasswordHash = dummyPasswordHash
	} else if err != nil {
		return LoginResult{}, err
	}
	valid, err := s.verifyPassword(ctx, password, a.PasswordHash)
	if err != nil {
		return LoginResult{}, err
	}
	if a.ID == 0 || a.Disabled || !valid {
		return LoginResult{}, ErrCredentials
	}

	var secret [keyBytes]byte
	rand.Read(secret[:])
	token := base64.RawURLEncoding.EncodeToString(secret[:])
	digest, err := tokenDigest(token)
	if err != nil {
		return LoginResult{}, err
	}
	lifetime := 24 * time.Hour
	if source == Native {
		lifetime = 30 * 24 * time.Hour
	}
	now := time.Now().UTC()
	expiresAt := now.Add(lifetime)
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current account
		err := tx.First(&current, a.ID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && (current.Disabled || current.SessionVersion != a.SessionVersion)) {
			return ErrCredentials
		}
		if err != nil {
			return err
		}
		if err := tx.Where("expires_at <= ?", now).Delete(&session{}).Error; err != nil {
			return err
		}
		return tx.Create(&session{
			Digest: digest, AccountID: a.ID, Source: source, Version: a.SessionVersion, ExpiresAt: expiresAt,
		}).Error
	})
	if err != nil {
		return LoginResult{}, err
	}
	return LoginResult{User: a.user(), Token: token, ExpiresAt: expiresAt}, nil
}

func authenticate(tx *gorm.DB, credential Credential) (account, error) {
	digest, err := tokenDigest(credential.Token)
	if err != nil {
		return account{}, err
	}
	var record session
	query := tx.Where("digest = ? AND source = ? AND revoked_at IS NULL AND expires_at > ?",
		digest, credential.Source, time.Now().UTC())
	if err := query.Take(&record).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return account{}, ErrSession
	} else if err != nil {
		return account{}, err
	}

	var a account
	err = tx.First(&a, record.AccountID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && (a.Disabled || a.SessionVersion != record.Version)) {
		return account{}, ErrSession
	}
	return a, err
}

func (s *Store) CurrentUser(ctx context.Context, credential Credential) (User, error) {
	var user User
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		a, err := authenticate(tx, credential)
		user = a.user()
		return err
	})
	return user, err
}

func (s *Store) Logout(ctx context.Context, credential Credential) error {
	digest, err := tokenDigest(credential.Token)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Model(&session{}).
		Where("digest = ? AND source = ? AND revoked_at IS NULL", digest, credential.Source).
		Update("revoked_at", time.Now().UTC()).Error
}

func (s *Store) ChangePassword(ctx context.Context, credential Credential, currentPassword, newPassword string) error {
	if err := validatePassword(newPassword); err != nil {
		return err
	}

	var a account
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		a, err = authenticate(tx, credential)
		return err
	})
	if err != nil {
		return err
	}
	valid, err := s.verifyPassword(ctx, currentPassword, a.PasswordHash)
	if err != nil {
		return err
	}
	if !valid {
		return ErrCredentials
	}
	hash, err := s.hashPassword(ctx, newPassword)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		current, err := authenticate(tx, credential)
		if err != nil {
			return err
		}
		if current.SessionVersion != a.SessionVersion {
			return ErrSession
		}
		return updatePassword(tx, &current, hash)
	})
}

func updatePassword(tx *gorm.DB, a *account, hash string) error {
	return tx.Model(a).Updates(map[string]any{
		"password_hash":   hash,
		"session_version": gorm.Expr("session_version + 1"),
	}).Error
}

func insertIfNew(tx *gorm.DB, row any, unique ...string) (bool, error) {
	columns := make([]clause.Column, len(unique))
	for i, name := range unique {
		columns[i] = clause.Column{Name: name}
	}
	result := tx.Clauses(clause.OnConflict{Columns: columns, DoNothing: true}).Create(row)
	return result.RowsAffected == 1, result.Error
}
