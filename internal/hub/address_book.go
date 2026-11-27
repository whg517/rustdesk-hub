package hub

import (
	"context"
	"errors"
	"strings"
	"unicode"

	"gorm.io/gorm"
)

type BookScope struct {
	Credential Credential
	GUID       string
}

type Peer struct {
	ID    string
	Alias string
	Note  string
	Tags  []string
}

type PeerChanges struct {
	Alias                    *string
	Note                     *string
	Tags                     *[]string
	ContainsDeviceCredential bool
}

type Tag struct {
	ID    uint
	Name  string
	Color uint32
}

type PeerQuery struct {
	Page        int
	PageSize    int
	Search      string
	Tag         string
	TagID       uint
	IncludeTags bool
}

type PeerPage struct {
	Total int64
	Peers []Peer
	Tags  []Tag
}

func (s *Store) withBook(ctx context.Context, scope BookScope, operation func(*gorm.DB, addressBook) error) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		a, err := authenticate(tx, scope.Credential)
		if err != nil {
			return err
		}
		query := tx.Where("account_id = ?", a.ID)
		if scope.GUID != "" {
			query = query.Where("guid = ?", scope.GUID)
		}
		var book addressBook
		if err := query.Take(&book).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		return operation(tx, book)
	})
}

func (s *Store) PersonalGUID(ctx context.Context, credential Credential) (string, error) {
	var guid string
	err := s.withBook(ctx, BookScope{Credential: credential}, func(_ *gorm.DB, book addressBook) error {
		guid = book.GUID
		return nil
	})
	return guid, err
}

func tagsInBook(tx *gorm.DB, book addressBook) ([]Tag, error) {
	var rows []tag
	if err := tx.Where("book_id = ?", book.ID).Order("name ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	tags := make([]Tag, 0, len(rows))
	for _, row := range rows {
		tags = append(tags, Tag{ID: row.ID, Name: row.Name, Color: row.Color})
	}
	return tags, nil
}

func (s *Store) ListTags(ctx context.Context, scope BookScope) ([]Tag, error) {
	var tags []Tag
	err := s.withBook(ctx, scope, func(tx *gorm.DB, book addressBook) error {
		var err error
		tags, err = tagsInBook(tx, book)
		return err
	})
	return tags, err
}

func (s *Store) ListPeers(ctx context.Context, scope BookScope, filter PeerQuery) (PeerPage, error) {
	var page PeerPage
	err := s.withBook(ctx, scope, func(tx *gorm.DB, book addressBook) error {
		query := tx.Model(&peer{}).Where("book_id = ?", book.ID)
		if filter.Search != "" {
			query = query.Where("(instr(lower(device_id), lower(?)) > 0 OR instr(lower(alias), lower(?)) > 0)",
				normalizeDeviceID(filter.Search), filter.Search)
		}
		if filter.TagID != 0 || filter.Tag != "" {
			matches := tx.Table("peer_tags").Select("peer_id").
				Joins("JOIN tags ON tags.id = peer_tags.tag_id").
				Where("tags.book_id = ?", book.ID)
			if filter.TagID != 0 {
				matches = matches.Where("tags.id = ?", filter.TagID)
			} else {
				matches = matches.Where("tags.name = ?", filter.Tag)
			}
			query = query.Where("id IN (?)", matches)
		}
		if err := query.Count(&page.Total).Error; err != nil {
			return err
		}
		var rows []peer
		offset := (filter.Page - 1) * filter.PageSize
		err := query.Order("device_id ASC").
			Offset(offset).Limit(filter.PageSize).
			Preload("Tags", func(q *gorm.DB) *gorm.DB { return q.Order("name ASC") }).
			Find(&rows).Error
		if err != nil {
			return err
		}
		page.Peers = make([]Peer, 0, len(rows))
		for _, row := range rows {
			tags := make([]string, 0, len(row.Tags))
			for _, label := range row.Tags {
				tags = append(tags, label.Name)
			}
			page.Peers = append(page.Peers, Peer{ID: row.DeviceID, Alias: row.Alias, Note: row.Note, Tags: tags})
		}
		if filter.IncludeTags {
			page.Tags, err = tagsInBook(tx, book)
		}
		return err
	})
	return page, err
}

const untaggedName = "Untagged"

func normalizeDeviceID(id string) string {
	return strings.ReplaceAll(id, " ", "")
}

func normalizeDeviceIDs(ids []string) ([]string, error) {
	normalized := make([]string, len(ids))
	for i, id := range ids {
		normalized[i] = normalizeDeviceID(id)
	}
	if err := validateLabels(normalized, InputError("RustDesk ID 无效")); err != nil {
		return nil, err
	}
	return normalized, nil
}

func (s *Store) SavedPeerIDs(ctx context.Context, scope BookScope, ids []string) ([]string, error) {
	normalized, err := normalizeDeviceIDs(ids)
	if err != nil {
		return nil, err
	}
	matches := make([]string, 0, len(normalized))
	err = s.withBook(ctx, scope, func(tx *gorm.DB, book addressBook) error {
		return tx.Model(&peer{}).Where("book_id = ? AND device_id IN ?", book.ID, normalized).
			Order("device_id ASC").Pluck("device_id", &matches).Error
	})
	return matches, err
}

func validLabel(value string) bool {
	return strings.TrimSpace(value) != "" && len(value) <= 256 && !strings.ContainsFunc(value, unicode.IsControl)
}

func validateLabels(values []string, message InputError) error {
	if len(values) > 1000 {
		return InputError("一次最多处理 1000 项")
	}
	for _, value := range values {
		if !validLabel(value) {
			return message
		}
	}
	return nil
}

func replacePeerTags(tx *gorm.DB, row peer, names []string) error {
	if err := validateLabels(names, InputError("标签名称无效")); err != nil {
		return err
	}
	namesSet := make(map[string]struct{}, len(names))
	for _, name := range names {
		namesSet[name] = struct{}{}
	}
	var tags []tag
	if len(names) > 0 {
		if err := tx.Where("book_id = ? AND name IN ?", row.BookID, names).Find(&tags).Error; err != nil {
			return err
		}
		if len(tags) != len(namesSet) {
			return InputError("指定的标签不存在，请刷新通讯录")
		}
	}
	if _, usesReservedName := namesSet[untaggedName]; usesReservedName {
		var existing int64
		err := tx.Table("peer_tags").
			Joins("JOIN tags ON tags.id = peer_tags.tag_id").
			Where("peer_tags.peer_id = ? AND tags.book_id = ? AND tags.name = ?", row.ID, row.BookID, untaggedName).
			Count(&existing).Error
		if err != nil {
			return err
		}
		if existing == 0 {
			return InputError("请将旧 Untagged 标签改名后再分配")
		}
	}
	if err := tx.Where("peer_id = ?", row.ID).Delete(&peerTag{}).Error; err != nil {
		return err
	}
	if len(tags) == 0 {
		return nil
	}
	links := make([]peerTag, 0, len(tags))
	for _, label := range tags {
		links = append(links, peerTag{PeerID: row.ID, TagID: label.ID})
	}
	return tx.Create(&links).Error
}

func (s *Store) AddPeer(ctx context.Context, scope BookScope, value Peer) (bool, error) {
	value.ID = normalizeDeviceID(value.ID)
	if !validLabel(value.ID) {
		return false, InputError("请填写有效的 RustDesk ID")
	}
	var created bool
	err := s.withBook(ctx, scope, func(tx *gorm.DB, book addressBook) error {
		row := peer{BookID: book.ID, DeviceID: value.ID, Alias: value.Alias, Note: value.Note}
		var err error
		created, err = insertIfNew(tx, &row, "book_id", "device_id")
		if err != nil || !created {
			return err
		}
		return replacePeerTags(tx, row, value.Tags)
	})
	return created && err == nil, err
}

func (s *Store) UpdatePeer(ctx context.Context, scope BookScope, deviceID string, changes PeerChanges) error {
	deviceID = normalizeDeviceID(deviceID)
	if !validLabel(deviceID) {
		return InputError("请填写有效的 RustDesk ID")
	}
	return s.withBook(ctx, scope, func(tx *gorm.DB, book addressBook) error {
		var row peer
		if err := tx.Where("book_id = ? AND device_id = ?", book.ID, deviceID).Take(&row).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		} else if err != nil {
			return err
		}

		fields := make(map[string]any, 2)
		if changes.Alias != nil {
			fields["alias"] = *changes.Alias
		}
		if changes.Note != nil {
			fields["note"] = *changes.Note
		}
		if len(fields) == 0 && changes.Tags == nil && changes.ContainsDeviceCredential {
			return ErrCredentialSync
		}
		if len(fields) > 0 {
			if err := tx.Model(&row).Updates(fields).Error; err != nil {
				return err
			}
		}
		if changes.Tags != nil {
			return replacePeerTags(tx, row, *changes.Tags)
		}
		return nil
	})
}

func (s *Store) DeletePeers(ctx context.Context, scope BookScope, ids []string) error {
	normalized, err := normalizeDeviceIDs(ids)
	if err != nil {
		return err
	}
	return s.withBook(ctx, scope, func(tx *gorm.DB, book addressBook) error {
		if len(normalized) == 0 {
			return nil
		}
		return tx.Where("book_id = ? AND device_id IN ?", book.ID, normalized).Delete(&peer{}).Error
	})
}

func validateNewTagName(name string) error {
	if name == untaggedName {
		return InputError("Untagged 是 RustDesk 的「无标签」筛选名称，请使用其他标签名称")
	}
	if !validLabel(name) {
		return InputError("请填写有效的标签名称")
	}
	return nil
}

func (s *Store) AddTag(ctx context.Context, scope BookScope, value Tag) (bool, error) {
	if err := validateNewTagName(value.Name); err != nil {
		return false, err
	}
	var created bool
	err := s.withBook(ctx, scope, func(tx *gorm.DB, book addressBook) error {
		var err error
		created, err = insertIfNew(tx, &tag{BookID: book.ID, Name: value.Name, Color: value.Color}, "book_id", "name")
		return err
	})
	return created && err == nil, err
}

type TagRef struct {
	ID   uint
	Name string
}

func (s *Store) UpdateTag(ctx context.Context, scope BookScope, ref TagRef, newName *string, color *uint32) error {
	if ref.ID == 0 && !validLabel(ref.Name) {
		return InputError("请填写有效的标签名称")
	}
	if newName != nil {
		if err := validateNewTagName(*newName); err != nil {
			return err
		}
	}
	return s.withBook(ctx, scope, func(tx *gorm.DB, book addressBook) error {
		var row tag
		query := tx.Where("book_id = ?", book.ID)
		if ref.ID != 0 {
			query = query.Where("id = ?", ref.ID)
		} else {
			query = query.Where("name = ?", ref.Name)
		}
		if err := query.Take(&row).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		} else if err != nil {
			return err
		}

		fields := make(map[string]any, 2)
		if newName != nil {
			fields["name"] = *newName
		}
		if color != nil {
			fields["color"] = *color
		}
		if len(fields) == 0 {
			return nil
		}
		if err := tx.Model(&row).Updates(fields).Error; errors.Is(err, gorm.ErrDuplicatedKey) {
			return ErrTagExists
		} else {
			return err
		}
	})
}

func (s *Store) DeleteTagByID(ctx context.Context, scope BookScope, id uint) error {
	return s.withBook(ctx, scope, func(tx *gorm.DB, book addressBook) error {
		return tx.Where("book_id = ? AND id = ?", book.ID, id).Delete(&tag{}).Error
	})
}

func (s *Store) DeleteTags(ctx context.Context, scope BookScope, names []string) error {
	if err := validateLabels(names, InputError("标签名称无效")); err != nil {
		return err
	}
	return s.withBook(ctx, scope, func(tx *gorm.DB, book addressBook) error {
		if len(names) == 0 {
			return nil
		}
		return tx.Where("book_id = ? AND name IN ?", book.ID, names).Delete(&tag{}).Error
	})
}
