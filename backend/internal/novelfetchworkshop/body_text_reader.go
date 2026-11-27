package novelfetchworkshop

import (
	"context"
	"errors"
	"strings"
	"time"
)

// ReadBodyText loads only the requested version. A missing independent record
// may fall back to explicit legacy text; storage errors must never become an
// empty body or trigger another fetch/model request.
func ReadBodyText(ctx context.Context, store BodyStore, owner string, document Document, versionID string) (string, error) {
	if store != nil {
		body, err := store.GetBody(ctx, owner, document.BookID, versionID)
		if err == nil {
			return body.Content, nil
		}
		if !errors.Is(err, ErrNotFound) {
			return "", err
		}
	}
	switch strings.ToLower(strings.TrimSpace(versionID)) {
	case "original":
		if document.Original != "" {
			return document.Original, nil
		}
		return document.ProcessedText, nil
	case "original_raw", "originalraw":
		return document.OriginalRaw, nil
	default:
		switch raw := document.Versions[versionID].(type) {
		case string:
			return raw, nil
		case map[string]any:
			content, _ := raw["text"].(string)
			return content, nil
		}
	}
	return "", nil
}

// bodyMetadataPatch is a JSON merge patch shared by both stores. It deliberately
// removes only the inline copy of the version just written and leaves raw text,
// other versions and unrelated task data intact.
func bodyMetadataPatch(ref BodyRef) map[string]any {
	patch := map[string]any{"bodyRefs": map[string]any{ref.VersionID: ref}, "meta": map[string]any{"autoSubmitReadyFingerprint": nil}}
	switch strings.ToLower(strings.TrimSpace(ref.VersionID)) {
	case "original":
		patch["original"] = nil
		patch["processedText"] = nil
		patch["processedChars"] = ref.CharCount
		patch["meta"].(map[string]any)["processedChars"] = ref.CharCount
	case "original_raw", "originalraw":
		patch["originalRaw"] = nil
		patch["originalRawChars"] = ref.CharCount
		patch["meta"].(map[string]any)["originalRawChars"] = ref.CharCount
	default:
		patch["versions"] = map[string]any{ref.VersionID: map[string]any{"text": nil, "versionId": ref.VersionID, "revision": ref.Revision, "contentHash": ref.ContentHash, "charCount": ref.CharCount, "updatedAt": ref.UpdatedAt}}
	}
	return patch
}

func documentWithBodyRef(document Document, ref BodyRef) Document {
	document = cloneDocument(normalizeDocument(document))
	return applyDocumentBodyRef(document, ref)
}

func applyDocumentBodyRef(document Document, ref BodyRef) Document {
	if document.BodyRefs[ref.VersionID].ContentHash != ref.ContentHash {
		delete(document.Meta, "autoSubmitReadyFingerprint")
	}
	document.BodyRefs[ref.VersionID] = ref
	switch strings.ToLower(strings.TrimSpace(ref.VersionID)) {
	case "original":
		document.Original = ""
		document.ProcessedText = ""
		document.ProcessedChars = int(ref.CharCount)
		document.Meta["processedChars"] = ref.CharCount
	case "original_raw", "originalraw":
		document.OriginalRaw = ""
		document.OriginalRawChars = int(ref.CharCount)
		document.Meta["originalRawChars"] = ref.CharCount
	default:
		metadata, _ := document.Versions[ref.VersionID].(map[string]any)
		if metadata == nil {
			metadata = map[string]any{}
		}
		patch := bodyMetadataPatch(ref)["versions"].(map[string]any)[ref.VersionID].(map[string]any)
		for key, value := range patch {
			if value == nil {
				delete(metadata, key)
			} else {
				metadata[key] = value
			}
		}
		document.Versions[ref.VersionID] = metadata
	}
	return document
}

func persistRewriteDocument(ctx context.Context, store Store, owner string, document Document) error {
	lightweight := cloneDocument(document)
	for _, ref := range document.BodyRefs {
		lightweight = applyDocumentBodyRef(lightweight, ref)
	}
	return store.PutDocument(ctx, owner, lightweight)
}

func previousDocumentTime(value string) time.Time {
	previous, _ := time.Parse(time.RFC3339Nano, value)
	return previous
}

// A reference carrying a persisted revision/hash proves this is not initial
// legacy inline text. If the current body row is gone, an older document write
// must not resurrect that body's reference or feed its old text to migration.
func pruneMissingBodyReferences(document Document, active map[string]bool) Document {
	for version, ref := range document.BodyRefs {
		if !active[version] && (ref.Revision > 0 || ref.ContentHash != "") {
			document = stripDocumentBodyVersion(document, version)
		}
	}
	return document
}

func stripDocumentBodyVersion(document Document, version string) Document {
	document = normalizeDocument(document)
	delete(document.BodyRefs, version)
	delete(document.Meta, "autoSubmitReadyFingerprint")
	switch version {
	case "original":
		document.Original = ""
		document.ProcessedText = ""
		document.ProcessedChars = 0
		document.Meta["processedChars"] = 0
	case "original_raw", "originalraw":
		document.OriginalRaw = ""
		document.OriginalRawChars = 0
		document.Meta["originalRawChars"] = 0
	default:
		if metadata, ok := document.Versions[version].(map[string]any); ok {
			for _, key := range []string{"text", "revision", "contentHash", "charCount", "updatedAt"} {
				delete(metadata, key)
			}
		} else {
			delete(document.Versions, version)
		}
	}
	return document
}

func bodyRemovalMetadataPatch(version string) map[string]any {
	patch := map[string]any{"meta": map[string]any{"autoSubmitReadyFingerprint": nil}}
	switch version {
	case "original":
		patch["original"] = nil
		patch["processedText"] = nil
		patch["processedChars"] = 0
		patch["meta"].(map[string]any)["processedChars"] = 0
	case "original_raw", "originalraw":
		patch["originalRaw"] = nil
		patch["originalRawChars"] = 0
		patch["meta"].(map[string]any)["originalRawChars"] = 0
	default:
		patch["versions"] = map[string]any{version: map[string]any{"text": nil, "revision": nil, "contentHash": nil, "charCount": nil, "updatedAt": nil}}
	}
	return patch
}
