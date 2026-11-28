package screenplay

import (
	"strings"
	"unicode/utf8"
)

const AssistantReferenceShotPageLimit = 100
const AssistantReferenceShotExcerptBytes = 256

type AssistantReferenceShotQuery struct {
	BoardID          string `json:"board_id"`
	ExpectedRevision int64  `json:"expected_revision"`
	ExpectedDigest   string `json:"expected_digest"`
	Offset           int    `json:"offset"`
	Limit            int    `json:"limit,omitempty"`
}
type AssistantReferenceBoardEvidence struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Revision int64  `json:"revision"`
	Digest   string `json:"digest"`
}
type AssistantReferenceShotEntry struct {
	ID               string `json:"id"`
	Seq              int    `json:"seq"`
	Position         int    `json:"position"`
	Excerpt          string `json:"excerpt"`
	ExcerptTruncated bool   `json:"excerpt_truncated"`
}
type AssistantReferenceShotPage struct {
	Board      AssistantReferenceBoardEvidence `json:"board"`
	Shots      []AssistantReferenceShotEntry   `json:"shots"`
	Offset     int                             `json:"offset"`
	NextOffset int                             `json:"next_offset"`
	Total      int                             `json:"total"`
}

// The full board digest signs every page, including large boards that cannot
// themselves be attached within the 16KiB reference budget. No Resolve here.
func (v AssistantReferenceView) ReferenceShotPage(q AssistantReferenceShotQuery) (AssistantReferenceShotPage, error) {
	out := AssistantReferenceShotPage{Shots: []AssistantReferenceShotEntry{}}
	if q.Offset < 0 || q.Limit < 0 || q.Limit > AssistantReferenceShotPageLimit {
		return out, ErrInvalidArgument
	}
	limit := q.Limit
	if limit == 0 {
		limit = AssistantReferenceShotPageLimit
	}
	whole, err := v.Describe(AssistantReferenceSelector{Kind: "storyboard", ID: q.BoardID})
	if err != nil {
		return out, err
	}
	if len(q.ExpectedDigest) != 64 || q.ExpectedDigest != whole.Digest || q.ExpectedRevision != whole.Revision {
		return out, ErrRevisionConflict
	}
	var shots []StoryboardShot
	for _, board := range v.Boards {
		if board.ID == q.BoardID {
			shots = board.Shots
			break
		}
	}
	if q.Offset > len(shots) {
		return out, ErrInvalidArgument
	}
	end := q.Offset + limit
	if end > len(shots) {
		end = len(shots)
	}
	out.Board = AssistantReferenceBoardEvidence{whole.ID, whole.Name, whole.Revision, whole.Digest}
	out.Offset = q.Offset
	out.NextOffset = end
	out.Total = len(shots)
	for i := q.Offset; i < end; i++ {
		s := shots[i]
		text := strings.Join([]string{s.VideoDesc, s.Line}, "\n")
		truncated := len(text) > AssistantReferenceShotExcerptBytes
		if truncated {
			n := AssistantReferenceShotExcerptBytes
			for n > 0 && !utf8.RuneStart(text[n]) {
				n--
			}
			text = text[:n]
		}
		out.Shots = append(out.Shots, AssistantReferenceShotEntry{s.ID, s.Seq, i, text, truncated})
	}
	return out, nil
}
