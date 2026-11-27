package novelfetchworkshop

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"strings"
)

const settingJournalVersion = 1

// Authentication covers the schema, request boundaries and edits together.
// Only the current request writer emits authoritative assignments and deletes.
type settingBatch struct {
	ID    string              `json:"batchId"`
	Edits []LegacySettingEdit `json:"edits"`
}

type settingJournal struct {
	SchemaVersion int                 `json:"schemaVersion"`
	Snapshots     []LegacySettingEdit `json:"snapshots,omitempty"`
	Batches       []settingBatch      `json:"batches"`
	historical    bool                // the original plaintext predates the envelope
}

func emptySettingJournal() settingJournal {
	return settingJournal{SchemaVersion: settingJournalVersion, Batches: []settingBatch{}}
}

func (j settingJournal) edits() []LegacySettingEdit {
	all := slices.Clone(j.Snapshots)
	for _, batch := range j.Batches {
		all = append(all, batch.Edits...)
	}
	return all
}

func historicalSettingEdits(input []LegacySettingEdit) ([]LegacySettingEdit, error) {
	var snapshot []LegacySettingEdit
	for _, edit := range input {
		if err := edit.Validate(); err != nil {
			return nil, err
		}
		if edit.Delete || channelIdentityEcho(edit) {
			continue
		}
		var value any
		if err := json.Unmarshal(edit.Value, &value); err != nil {
			return nil, err
		}
		var field []string
		if len(edit.Path) >= 4 && edit.Path[0] == "apiIntegrations" && edit.Path[1] == "customChannels" {
			field = edit.Path[3:]
		}
		if historicalChannelDefault(field, value) {
			continue
		}
		edit.OnlyIfMissing = true
		snapshot = append(snapshot, edit)
	}
	return snapshot, nil
}

// A partially marked or unknown envelope must never fall back to the map importer.
func parseSettingJournal(raw []byte, convertMap func(map[string]any) []LegacySettingEdit) (settingJournal, error) {
	j := emptySettingJournal()
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return j, errors.New("empty setting journal")
	}
	switch raw[0] {
	case '[':
		var input []LegacySettingEdit
		if err := json.Unmarshal(raw, &input); err != nil {
			return j, err
		}
		j.historical = true
		var err error
		j.Snapshots, err = historicalSettingEdits(input)
		return j, err
	case '{':
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return j, err
		}
		if fields["schemaVersion"] != nil || fields["batches"] != nil || fields["snapshots"] != nil {
			var marked settingJournal
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&marked); err != nil {
				return j, err
			}
			return marked, marked.validate()
		}
		if convertMap != nil {
			var old map[string]any
			if err := json.Unmarshal(raw, &old); err != nil {
				return j, err
			}
			j.historical = true
			j.Snapshots = convertMap(old)
			return j, j.validate()
		}
	}
	return j, errors.New("unrecognized setting journal format")
}

func (j settingJournal) validate() error {
	if j.SchemaVersion != settingJournalVersion || j.Batches == nil {
		return errors.New("unsupported or incomplete setting journal")
	}
	for _, e := range j.Snapshots {
		if !e.OnlyIfMissing || channelIdentityEcho(e) {
			return errors.New("invalid snapshot assignment")
		}
		if err := e.Validate(); err != nil {
			return err
		}
	}
	ids := make(map[string]bool, len(j.Batches))
	for _, batch := range j.Batches {
		if strings.TrimSpace(batch.ID) == "" || ids[batch.ID] || len(batch.Edits) == 0 {
			return errors.New("invalid setting request batch")
		}
		ids[batch.ID] = true
		for _, e := range batch.Edits {
			if e.OnlyIfMissing || channelIdentityEcho(e) {
				return errors.New("invalid request assignment")
			}
			if err := e.Validate(); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *MySQLStore) openSettingJournal(owner string, state LegacyConfigMigrationState) (settingJournal, error) {
	if state.PatchEnvelope == "" && state.PatchFingerprint == "" {
		return emptySettingJournal(), nil
	}
	if s.ModelSecrets == nil {
		return settingJournal{}, errors.New("legacy setting patch requires encryption")
	}
	raw, err := s.ModelSecrets.OpenLegacyConflict(owner, settingPatchBinding(), state.PatchEnvelope, state.PatchFingerprint)
	if err != nil {
		return settingJournal{}, err
	}
	return parseSettingJournal([]byte(raw), nil)
}

// Preserve conditional snapshot fields separately. Across explicit batches
// keep the last edit for each exact path, in its original request and order.
func combineSettingJournals(older, newer settingJournal) (settingJournal, error) {
	j := settingJournal{SchemaVersion: settingJournalVersion, Snapshots: append(older.Snapshots, newer.Snapshots...), Batches: append(older.Batches, newer.Batches...)}
	if err := j.validate(); err != nil {
		return j, err
	}
	seen := map[string]bool{}
	for i := len(j.Batches) - 1; i >= 0; i-- {
		batch := &j.Batches[i]
		var keep []LegacySettingEdit
		for k := len(batch.Edits) - 1; k >= 0; k-- {
			e := batch.Edits[k]
			path, _ := json.Marshal(e.Path)
			if !seen[string(path)] {
				keep = append(keep, e)
				seen[string(path)] = true
			}
		}
		slices.Reverse(keep)
		batch.Edits = keep
	}
	j.Batches = slices.DeleteFunc(j.Batches, func(b settingBatch) bool { return len(b.Edits) == 0 })
	return j, nil
}
