package screenplay

import "strings"

// AssistantReferenceScope is server-only. Construct from authenticated identities,
// never from a selector. Construction does not replace current permission guards.
type AssistantReferenceScope struct{ owner, project, session, effective, actual string }

func NewAssistantReferenceScope(owner, project, session, effective, actual string) (AssistantReferenceScope, error) {
	s := AssistantReferenceScope{owner, project, session, effective, actual}
	return s, s.ValidateRead()
}
func (s AssistantReferenceScope) ValidateRead() error {
	for _, v := range []string{s.owner, s.project, s.session, s.effective} {
		if v == "" || v != strings.TrimSpace(v) || len(v) > 191 || strings.ContainsAny(v, "\x00\r\n") {
			return ErrAccessDenied
		}
	}
	if len(s.project) > 96 || len(s.session) > 96 || len(s.actual) > 191 || s.actual != strings.TrimSpace(s.actual) || strings.ContainsAny(s.actual, "\x00\r\n") {
		return ErrAccessDenied
	}
	return nil
}
func (s AssistantReferenceScope) ValidatePrepare() error {
	if s.ValidateRead() != nil || s.actual == "" {
		return ErrAccessDenied
	}
	return nil
}
