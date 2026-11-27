package hub

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

func requireAdmin(tx *gorm.DB, credential Credential) error {
	a, err := authenticate(tx, credential)
	if err != nil {
		return err
	}
	if a.ID != ownerID {
		return ErrAdminRequired
	}
	return nil
}

func (s *Store) withAdmin(ctx context.Context, credential Credential, operation func(*gorm.DB) error) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := requireAdmin(tx, credential); err != nil {
			return err
		}
		return operation(tx)
	})
}

func (s *Store) ListUsers(ctx context.Context, credential Credential) ([]User, error) {
	users := make([]User, 0)
	err := s.withAdmin(ctx, credential, func(tx *gorm.DB) error {
		var rows []account
		if err := tx.Select("id", "name", "disabled").Order("id").Find(&rows).Error; err != nil {
			return err
		}
		for _, a := range rows {
			users = append(users, a.user())
		}
		return nil
	})
	return users, err
}

func (s *Store) CreateUser(ctx context.Context, credential Credential, name, password string) (User, error) {
	if err := validateUsername(name); err != nil {
		return User{}, err
	}
	hash, err := s.hashUserPassword(ctx, credential, password)
	if err != nil {
		return User{}, err
	}
	a := account{Name: name, PasswordHash: hash, SessionVersion: 1}
	err = s.withAdmin(ctx, credential, func(tx *gorm.DB) error {
		return createAccount(tx, &a)
	})
	return a.user(), err
}

func (s *Store) SetUserDisabled(ctx context.Context, credential Credential, id uint, disabled bool) error {
	return s.withAdmin(ctx, credential, func(tx *gorm.DB) error {
		a, err := memberByID(tx, id)
		if err != nil {
			return err
		}
		if a.Disabled == disabled {
			return nil
		}
		return tx.Model(&a).Updates(map[string]any{
			"disabled":        disabled,
			"session_version": gorm.Expr("session_version + 1"),
		}).Error
	})
}

func (s *Store) ResetUserPassword(ctx context.Context, credential Credential, id uint, password string) error {
	hash, err := s.hashUserPassword(ctx, credential, password)
	if err != nil {
		return err
	}
	return s.withAdmin(ctx, credential, func(tx *gorm.DB) error {
		a, err := memberByID(tx, id)
		if err != nil {
			return err
		}
		return updatePassword(tx, &a, hash)
	})
}

// Verify permission before hashing outside the write transaction.
// withAdmin checks the session again before the corresponding write.
func (s *Store) hashUserPassword(ctx context.Context, credential Credential, password string) (string, error) {
	if err := requireAdmin(s.db.WithContext(ctx), credential); err != nil {
		return "", err
	}
	if err := validatePassword(password); err != nil {
		return "", err
	}
	return s.hashPassword(ctx, password)
}

func memberByID(tx *gorm.DB, id uint) (account, error) {
	if id == ownerID {
		return account{}, InputError("请选择普通用户")
	}
	var a account
	err := tx.First(&a, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return account{}, ErrUserNotFound
	}
	return a, err
}
