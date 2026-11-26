package batchfactoryv11

import (
	"reflect"
	"strings"
	"testing"
)

func TestComicStyleSQLScopeBindsBeforeLimit(t *testing.T) {
	injected := "style' OR 1=1 --"
	query, args := scopeComicStyleSQL("SELECT p.id FROM batch_factory_v12_comic_pages p WHERE p.owner_username=? ORDER BY p.updated_at LIMIT ?", []any{"alice", 12}, injected)
	if strings.Contains(query, injected) || !strings.Contains(query, "AND p.style_id=? ORDER BY") || !reflect.DeepEqual(args, []any{"alice", injected, 12}) {
		t.Fatalf("unsafe or misplaced style scope: %s %+v", query, args)
	}
}
