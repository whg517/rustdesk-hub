package hub

import (
	"context"
	"time"

	"gorm.io/gorm"
)

const diagnosticLimit = 100

type Diagnostic struct {
	ID        uint
	Time      time.Time
	Source    Source
	Operation string
	Status    int
	ErrorCode string `gorm:"not null;default:''"`
	RequestID string
}

func (s *Store) RecordDiagnostic(ctx context.Context, result Diagnostic) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&result).Error; err != nil {
			return err
		}
		keep := tx.Model(&Diagnostic{}).Select("id").Order("id DESC").Limit(diagnosticLimit)
		return tx.Where("id NOT IN (?)", keep).Delete(&Diagnostic{}).Error
	})
}

func (s *Store) RecentDiagnostics(ctx context.Context, credential Credential) ([]Diagnostic, error) {
	results := make([]Diagnostic, 0)
	err := s.withAdmin(ctx, credential, func(tx *gorm.DB) error {
		return tx.Order("id DESC").Limit(diagnosticLimit).Find(&results).Error
	})
	return results, err
}
