package batchfactoryv11

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type ComicLegacyStyleStore interface {
	AdoptLegacyComicStyle(context.Context, string, string, string, ComicStyleOutput) error
	ComicStyleStorageID(context.Context, string, string, string, string) (string, error)
}

// Bind the historical empty scope to its original display style, without moving
// rows. In-flight jobs that captured the old scope can still persist safely.
func (s *MemoryStore) AdoptLegacyComicStyle(ctx context.Context, owner, batchID, bookID string, style ComicStyleOutput) error {
	if strings.TrimSpace(style.ID) == "" {
		return ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.ownedBookLocked(owner, batchID, bookID); err != nil {
		return err
	}
	if s.comicLegacyStyles == nil {
		s.comicLegacyStyles = map[[3]string]ComicStyleOutput{}
	}
	key := [3]string{owner, batchID, bookID}
	if _, found := s.comicLegacyStyles[key]; found {
		return nil
	}
	for _, entry := range s.comicPages {
		if entry.Owner == owner && entry.Value.BatchID == batchID && entry.Value.BookID == bookID && entry.Value.StyleID == style.ID {
			return ErrConflict
		}
	}
	if _, found := s.comicLegacyStyles[key]; !found {
		s.comicLegacyStyles[key] = style
	}
	return nil
}
func (s *MemoryStore) ComicStyleStorageID(ctx context.Context, owner, batchID, bookID, styleID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.ownedBookLocked(owner, batchID, bookID); err != nil {
		return "", err
	}
	if legacy, ok := s.comicLegacyStyles[[3]string{owner, batchID, bookID}]; ok && legacy.ID == styleID {
		return "", nil
	}
	return styleID, nil
}
func (s *MySQLStore) AdoptLegacyComicStyle(ctx context.Context, owner, batchID, bookID string, style ComicStyleOutput) error {
	if strings.TrimSpace(style.ID) == "" {
		return ErrInvalid
	}
	snapshot, err := json.Marshal(style)
	if err != nil {
		return err
	}
	if err := ensureBookOwnership(ctx, s.db, owner, batchID, bookID); err != nil {
		return err
	}
	var alreadyBound string
	lookupErr := s.db.QueryRowContext(ctx, `SELECT style_id FROM batch_factory_v12_comic_style_outputs WHERE owner_username=? AND batch_id=? AND book_id=? AND is_legacy=TRUE LIMIT 1`, owner, batchID, bookID).Scan(&alreadyBound)
	if lookupErr == nil {
		return nil
	}
	if !errors.Is(lookupErr, sql.ErrNoRows) {
		return lookupErr
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = ensureBookOwnership(ctx, tx, owner, batchID, bookID); err != nil {
		return err
	}
	// Serialize first binding by the parent book, not by a potentially absent row.
	var locked string
	if err = tx.QueryRowContext(ctx, `SELECT id FROM batch_factory_v12_books WHERE id=? AND batch_id=? AND owner_username=? FOR UPDATE`, bookID, batchID, owner).Scan(&locked); err != nil {
		return err
	}
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT style_id FROM batch_factory_v12_comic_style_outputs WHERE owner_username=? AND batch_id=? AND book_id=? AND is_legacy=TRUE LIMIT 1`, owner, batchID, bookID).Scan(&existing)
	if err == nil {
		return tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	// An output already produced under this physical ID cannot also represent the
	// historical scope. Keep both datasets unchanged for explicit reconciliation.
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM batch_factory_v12_comic_pages WHERE owner_username=? AND batch_id=? AND book_id=? AND style_id=?`, owner, batchID, bookID, style.ID).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return ErrConflict
	}
	now := time.Now().UTC()
	if _, err = tx.ExecContext(ctx, `INSERT INTO batch_factory_v12_comic_style_outputs(owner_username,batch_id,book_id,style_id,style_name,snapshot_json,is_legacy,created_at,updated_at) VALUES(?,?,?,?,?,?,TRUE,?,?)`, owner, batchID, bookID, style.ID, style.Name, string(snapshot), now, now); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *MySQLStore) ComicStyleStorageID(ctx context.Context, owner, batchID, bookID, styleID string) (string, error) {
	if err := ensureBookOwnership(ctx, s.db, owner, batchID, bookID); err != nil {
		return "", err
	}
	var legacy string
	err := s.db.QueryRowContext(ctx, `SELECT style_id FROM batch_factory_v12_comic_style_outputs WHERE owner_username=? AND batch_id=? AND book_id=? AND is_legacy=TRUE LIMIT 1`, owner, batchID, bookID).Scan(&legacy)
	if errors.Is(err, sql.ErrNoRows) {
		return styleID, nil
	}
	if err != nil {
		return "", err
	}
	if legacy == styleID {
		return "", nil
	}
	return styleID, nil
}

func ResolveComicStyleContext(ctx context.Context, store ComicPageStore, owner, batchID, bookID, styleID string) (context.Context, error) {
	physical := styleID
	if resolver, ok := store.(ComicLegacyStyleStore); ok {
		var err error
		physical, err = resolver.ComicStyleStorageID(ctx, owner, batchID, bookID, styleID)
		if err != nil {
			return nil, err
		}
	}
	return context.WithValue(ctx, comicStyleContextKey{}, comicStyleScope{Logical: styleID, Physical: physical}), nil
}
