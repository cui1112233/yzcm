package batchfactoryv11

import "context"

type ComicStyleScopedStore interface{ ForComicStyle(string) ComicPageStore }
type comicStyleContextKey struct{}
type comicStyleScope struct{ Logical, Physical string }

func ComicStyleContext(ctx context.Context, styleID string) context.Context {
	return context.WithValue(ctx, comicStyleContextKey{}, comicStyleScope{Logical: styleID, Physical: styleID})
}
func comicStyleID(ctx context.Context) string {
	scope, _ := ctx.Value(comicStyleContextKey{}).(comicStyleScope)
	return scope.Physical
}
func ComicStyleOutputID(ctx context.Context) string {
	scope, _ := ctx.Value(comicStyleContextKey{}).(comicStyleScope)
	return scope.Logical
}
func comicStyleScopePresent(ctx context.Context) bool {
	return ctx.Value(comicStyleContextKey{}) != nil
}

// Each adapter owns an immutable selection. The underlying store and its mutex
// are shared; selecting a style never copies or replaces the storage mutex.
type comicStyleStore struct {
	base    ComicPageStore
	styleID string
}

func (s *MemoryStore) ForComicStyle(id string) ComicPageStore {
	return &comicStyleStore{base: s, styleID: id}
}
func (s *MySQLStore) ForComicStyle(id string) ComicPageStore {
	return &comicStyleStore{base: s, styleID: id}
}
func (s *comicStyleStore) scope(ctx context.Context, o, b, k string) (context.Context, error) {
	return ResolveComicStyleContext(ctx, s.base, o, b, k, s.styleID)
}
func (s *comicStyleStore) UpsertComicPage(ctx context.Context, o, b, k string, v string, in UpsertComicPageInput) (ComicPage, error) {
	var err error
	ctx, err = s.scope(ctx, o, b, k)
	if err != nil {
		return ComicPage{}, err
	}
	value, err := s.base.UpsertComicPage(ctx, o, b, k, v, in)
	if err == nil {
		value.StyleID = s.styleID
	}
	return value, err
}
func (s *comicStyleStore) PatchComicPage(ctx context.Context, o, b, k string, v string, in PatchComicPageInput) (ComicPage, error) {
	var err error
	ctx, err = s.scope(ctx, o, b, k)
	if err != nil {
		return ComicPage{}, err
	}
	patcher, ok := s.base.(ComicPagePatcher)
	if !ok {
		return ComicPage{}, ErrUnavailable
	}
	value, err := patcher.PatchComicPage(ctx, o, b, k, v, in)
	if err == nil {
		value.StyleID = s.styleID
	}
	return value, err
}
func (s *comicStyleStore) ListComicPages(ctx context.Context, o, b, k string) ([]ComicPage, error) {
	var err error
	ctx, err = s.scope(ctx, o, b, k)
	if err != nil {
		return nil, err
	}
	value, err := s.base.ListComicPages(ctx, o, b, k)
	if err == nil {
		for i := range value {
			value[i].StyleID = s.styleID
		}
	}
	return value, err
}
func (s *comicStyleStore) CreateComicPageImage(ctx context.Context, o, b, k string, p string, in CreateComicPageImageInput) (ComicPageImage, error) {
	var err error
	ctx, err = s.scope(ctx, o, b, k)
	if err != nil {
		return ComicPageImage{}, err
	}
	value, err := s.base.CreateComicPageImage(ctx, o, b, k, p, in)
	return value, err
}
func (s *comicStyleStore) SetPrimaryComicPageImage(ctx context.Context, o, b, k string, p, i string) (ComicPageImage, error) {
	var err error
	ctx, err = s.scope(ctx, o, b, k)
	if err != nil {
		return ComicPageImage{}, err
	}
	value, err := s.base.SetPrimaryComicPageImage(ctx, o, b, k, p, i)
	return value, err
}
func (s *comicStyleStore) SetComicPageReferenceImages(ctx context.Context, o, b, k string, v string, urls []string) (ComicPage, error) {
	var err error
	ctx, err = s.scope(ctx, o, b, k)
	if err != nil {
		return ComicPage{}, err
	}
	value, err := s.base.SetComicPageReferenceImages(ctx, o, b, k, v, urls)
	if err == nil {
		value.StyleID = s.styleID
	}
	return value, err
}
func (s *comicStyleStore) ApplyComicPageReferenceImages(ctx context.Context, o, b, k string, v string) (int, error) {
	var err error
	ctx, err = s.scope(ctx, o, b, k)
	if err != nil {
		return 0, err
	}
	value, err := s.base.ApplyComicPageReferenceImages(ctx, o, b, k, v)
	return value, err
}
func (s *comicStyleStore) ListComicCollections(ctx context.Context, o, b, k string) ([]ComicCollection, error) {
	var err error
	ctx, err = s.scope(ctx, o, b, k)
	if err != nil {
		return nil, err
	}
	value, err := s.base.ListComicCollections(ctx, o, b, k)
	if err == nil {
		for i := range value {
			value[i].StyleID = s.styleID
		}
	}
	return value, err
}
func (s *comicStyleStore) CreateComicCollection(ctx context.Context, o, b, k string, v ...[]string) (ComicCollection, []string, error) {
	ctx, err := s.scope(ctx, o, b, k)
	if err != nil {
		return ComicCollection{}, nil, err
	}
	value, missing, err := s.base.CreateComicCollection(ctx, o, b, k, v...)
	if err == nil {
		value.StyleID = s.styleID
	}
	return value, missing, err
}
func (s *comicStyleStore) ListComicPagesLite(ctx context.Context, o, b, k string) ([]ComicPage, error) {
	ctx, err := s.scope(ctx, o, b, k)
	if err != nil {
		return nil, err
	}
	var pages []ComicPage
	if lite, ok := s.base.(interface {
		ListComicPagesLite(context.Context, string, string, string) ([]ComicPage, error)
	}); ok {
		pages, err = lite.ListComicPagesLite(ctx, o, b, k)
	} else {
		pages, err = s.base.ListComicPages(ctx, o, b, k)
	}
	if err == nil {
		for i := range pages {
			pages[i].StyleID = s.styleID
		}
	}
	return pages, err
}
