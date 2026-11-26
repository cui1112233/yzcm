package batchfactoryv11

import (
	"context"
	"database/sql"
	"errors"
)

type ComicStyleSettingsReader interface {
	ComicStyleSettings(context.Context, string, string, string) (SettingsPatch, error)
	ComicStyleOutputExists(context.Context, string, string, string, string) (bool, error)
}

func (s *MySQLStore) ComicStyleSettings(ctx context.Context, o, b, k string) (SettingsPatch, error) {
	if err := ensureBookOwnership(ctx, s.db, o, b, k); err != nil {
		return nil, err
	}
	return effectiveSettings(ctx, s.db, o, ScopeRef{Kind: ScopeBook, BatchID: b, BookID: k})
}
func (s *MemoryStore) ComicStyleSettings(ctx context.Context, o, b, k string) (SettingsPatch, error) {
	batch, err := s.GetBatch(ctx, o, b)
	if err != nil {
		return nil, err
	}
	for _, book := range batch.Books {
		if book.ID == k {
			return ResolveSettings(batch.SettingsState.Patch, book.SettingsState.Patch), nil
		}
	}
	return nil, ErrNotFound
}
func (s *MySQLStore) ComicStyleOutputExists(ctx context.Context, o, b, k, id string) (bool, error) {
	physical, err := s.ComicStyleStorageID(ctx, o, b, k, id)
	if err != nil {
		return false, err
	}
	var count int
	err = s.db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM batch_factory_v12_comic_pages WHERE owner_username=? AND batch_id=? AND book_id=? AND style_id=?)+(SELECT COUNT(*) FROM batch_factory_v12_comic_collections WHERE owner_username=? AND batch_id=? AND book_id=? AND style_id=?)`, o, b, k, physical, o, b, k, physical).Scan(&count)
	return count > 0, err
}
func (s *MemoryStore) ComicStyleOutputExists(ctx context.Context, o, b, k, id string) (bool, error) {
	physical, err := s.ComicStyleStorageID(ctx, o, b, k, id)
	if err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.comicPages {
		if p.Owner == o && p.Value.BatchID == b && p.Value.BookID == k && p.Value.StyleID == physical {
			return true, nil
		}
	}
	for _, c := range s.comicCollections {
		if c.Owner == o && c.Value.BatchID == b && c.Value.BookID == k && c.Value.StyleID == physical {
			return true, nil
		}
	}
	return false, nil
}

// The logical ID is the selector identity; the physical ID remains stable for
// old in-flight jobs and is used for leases, repaired-prompt and image keys.
func ComicStyleStorageScopeID(ctx context.Context) string { return comicStyleID(ctx) }

// Immutable page/image IDs are safe to read without the current UI selection.
// Ownership and both parent IDs are checked before returning the image.
func (s *MemoryStore) GetComicPageImage(_ context.Context, o, b, k, pageID, imageID string) (ComicPageImage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.comicPages[pageID]
	if !ok || p.Owner != o || p.Value.BatchID != b || p.Value.BookID != k {
		return ComicPageImage{}, ErrNotFound
	}
	image, exists := s.comicPageImages[imageID]
	if exists && image.Owner == o && image.Value.ComicPageID == pageID {
		return image.Value, nil
	}
	return ComicPageImage{}, ErrNotFound
}

func (s *MemoryStore) GetBookAssetImage(_ context.Context, o, b, k, assetID, imageID string) (BookAssetImage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hasBookLocked(o, b, k) || !s.hasAssetLocked(o, b, k, assetID) {
		return BookAssetImage{}, ErrNotFound
	}
	image, ok := s.bookAssetImages[imageID]
	if !ok || image.Owner != o || image.Value.AssetID != assetID {
		return BookAssetImage{}, ErrNotFound
	}
	return image.Value, nil
}
func (s *MySQLStore) GetBookAssetImage(ctx context.Context, o, b, k, assetID, imageID string) (BookAssetImage, error) {
	if err := ensureBookAssetOwnership(ctx, s.db, o, b, k, assetID); err != nil {
		return BookAssetImage{}, err
	}
	var image BookAssetImage
	err := s.db.QueryRowContext(ctx, `SELECT id,url,COALESCE(storage_ref,''),media_type,source,is_primary,revision,created_at,style_id FROM batch_factory_v12_book_asset_images WHERE owner_username=? AND asset_id=? AND id=?`, o, assetID, imageID).Scan(&image.ID, &image.URL, &image.StorageRef, &image.MediaType, &image.Source, &image.IsPrimary, &image.Revision, &image.CreatedAt, &image.StyleID)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	image.AssetID = assetID
	return image, err
}
