package novelfetchworkshop

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

type settingSource struct {
	binding                 LegacyConflictSource
	ciphertext, fingerprint string
}

func (s *MySQLStore) retainSettingSource(ctx context.Context, tx *sql.Tx, owner string, src settingSource) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO novel_fetch_legacy_setting_files(owner_username,fingerprint,source_ciphertext) VALUES(?,?,?)
ON DUPLICATE KEY UPDATE source_ciphertext=COALESCE(source_ciphertext,?)`, owner, src.fingerprint, src.ciphertext, src.ciphertext)
	if err != nil {
		return err
	}
	var stored string
	if err = tx.QueryRowContext(ctx, `SELECT source_ciphertext FROM novel_fetch_legacy_setting_files WHERE owner_username=? AND fingerprint=?`, owner, src.fingerprint).Scan(&stored); err != nil {
		return err
	}
	_, err = s.ModelSecrets.OpenLegacyConflict(owner, src.binding, stored, src.fingerprint)
	return err
}

func (s *MySQLStore) retainOldDBPatch(ctx context.Context, tx *sql.Tx, state LegacyConfigMigrationState, j settingJournal) error {
	if !j.historical {
		return nil
	}
	return s.retainSettingSource(ctx, tx, state.Owner, settingSource{settingPatchBinding(), state.PatchEnvelope, strings.ToLower(state.PatchFingerprint)})
}

func (s *MySQLStore) AppendLegacySettingEdits(ctx context.Context, owner string, edits []LegacySettingEdit) error {
	j := emptySettingJournal()
	if len(edits) > 0 {
		j.Batches = append(j.Batches, settingBatch{ID: rand.Text(), Edits: edits})
	}
	return s.mergeSettingJournal(ctx, owner, j, nil)
}

func (s *MySQLStore) ImportLegacySettingFile(ctx context.Context, owner, ciphertext, fingerprint string, discover bool, convertMap func(map[string]any) []LegacySettingEdit) error {
	digest, err := hex.DecodeString(fingerprint)
	if err != nil || len(digest) != 32 {
		return errors.New("invalid legacy file identity")
	}
	src := settingSource{LegacyConflictSource{Document: "deferred-settings.json", Field: "settings"}, ciphertext, hex.EncodeToString(digest)}
	if s.ModelSecrets == nil {
		return errors.New("legacy settings import requires encryption")
	}
	plain, err := s.ModelSecrets.OpenLegacyConflict(owner, src.binding, ciphertext, src.fingerprint)
	if err != nil {
		return err
	}
	j, err := parseSettingJournal([]byte(plain), convertMap)
	if err != nil {
		return err
	}
	if discover {
		if _, _, err = s.DiscoverLegacyConfigMigration(ctx, owner); err != nil {
			return err
		}
	}
	return s.mergeSettingJournal(ctx, owner, j, &src)
}

func (s *MySQLStore) mergeSettingJournal(ctx context.Context, owner string, incoming settingJournal, source *settingSource) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if source != nil {
		_, err = tx.ExecContext(ctx, `INSERT INTO novel_fetch_legacy_config_migrations(owner_username,phase) VALUES(?,'finalizing')
ON DUPLICATE KEY UPDATE revision=revision`, owner)
		if err != nil {
			return err
		}
	}
	state, found, err := readMigrationState(tx.QueryRowContext(ctx, migrationStateQuery+" FOR UPDATE", owner))
	if err != nil {
		return err
	}
	if !found {
		return ErrConfigConflict
	}
	if source != nil {
		var imported bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM novel_fetch_legacy_setting_files WHERE owner_username=? AND fingerprint=?)`, owner, source.fingerprint).Scan(&imported); err != nil {
			return err
		}
		// Every file using the old purpose is recoverable evidence, including
		// files whose receipts were first issued with only a fingerprint.
		if err = s.retainSettingSource(ctx, tx, owner, *source); err != nil {
			return err
		}
		if imported {
			return tx.Commit()
		}
		if state.Phase == LegacyComplete {
			if _, err = tx.ExecContext(ctx, `UPDATE novel_fetch_legacy_config_migrations SET phase='finalizing',revision=revision+1,updated_at=CURRENT_TIMESTAMP(6) WHERE owner_username=?`, owner); err != nil {
				return err
			}
		}
	} else if state.Phase == LegacyComplete {
		return ErrConfigConflict
	}
	if len(incoming.Snapshots) == 0 && len(incoming.Batches) == 0 {
		return tx.Commit()
	}
	previous, err := s.openSettingJournal(owner, state)
	if err != nil {
		return err
	}
	if err = s.retainOldDBPatch(ctx, tx, state, previous); err != nil {
		return err
	}
	older, newer := previous, incoming
	if source != nil {
		older, newer = incoming, previous
	}
	combined, err := combineSettingJournals(older, newer)
	if err != nil {
		return err
	}
	if s.ModelSecrets == nil {
		return errors.New("legacy setting patch requires encryption")
	}
	raw, err := json.Marshal(combined)
	if err != nil {
		return err
	}
	ciphertext, fingerprint, err := s.ModelSecrets.SealLegacyConflict(owner, settingPatchBinding(), string(raw))
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE novel_fetch_legacy_config_migrations SET patch_ciphertext=?,patch_fingerprint=?,revision=revision+1,updated_at=CURRENT_TIMESTAMP(6) WHERE owner_username=?`, ciphertext, fingerprint, owner)
	if err != nil {
		return err
	}
	if source != nil {
		stored, _, err := readMigrationState(tx.QueryRowContext(ctx, migrationStateQuery, owner))
		if err != nil {
			return err
		}
		verified, err := s.ModelSecrets.OpenLegacyConflict(owner, settingPatchBinding(), stored.PatchEnvelope, stored.PatchFingerprint)
		if err != nil {
			return err
		}
		if verified != string(raw) {
			return errors.New("imported setting journal failed verification")
		}
	}
	return tx.Commit()
}
