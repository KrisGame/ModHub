package storage

import "modhub/internal/domain"

func (d *Database) UpsertAccount(rec domain.AccountRecord) error {
	if d == nil {
		return nil
	}
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	if d.closed.Load() {
		return nil
	}
	actInt := 0
	if rec.Active {
		actInt = 1
	}
	tx, err := d.writeDB.Begin()
	if err != nil {
		return err
	}
	if rec.Active {
		if _, err := tx.Exec(`UPDATE accounts SET active = 0`); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	acctType := rec.AccountType
	if acctType == "" {
		acctType = "microsoft"
	}
	_, err = tx.Exec(`
		INSERT INTO accounts (
			id, account_type, microsoft_account_id, xbox_user_id, xbox_gamertag,
			minecraft_uuid, minecraft_username, profile_name,
			skin_url, skin_variant, cape_url, cape_alias,
			auth_status, license_status,
			last_auth_at, last_license_check_at, profile_updated_at,
			ms_expires_at, xbl_expires_at, xsts_expires_at, mc_expires_at,
			vault_key, last_error, active, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			account_type=excluded.account_type,
			microsoft_account_id=excluded.microsoft_account_id,
			xbox_user_id=excluded.xbox_user_id,
			xbox_gamertag=excluded.xbox_gamertag,
			minecraft_uuid=excluded.minecraft_uuid,
			minecraft_username=excluded.minecraft_username,
			profile_name=excluded.profile_name,
			skin_url=excluded.skin_url,
			skin_variant=excluded.skin_variant,
			cape_url=excluded.cape_url,
			cape_alias=excluded.cape_alias,
			auth_status=excluded.auth_status,
			license_status=excluded.license_status,
			last_auth_at=excluded.last_auth_at,
			last_license_check_at=excluded.last_license_check_at,
			profile_updated_at=excluded.profile_updated_at,
			ms_expires_at=excluded.ms_expires_at,
			xbl_expires_at=excluded.xbl_expires_at,
			xsts_expires_at=excluded.xsts_expires_at,
			mc_expires_at=excluded.mc_expires_at,
			vault_key=excluded.vault_key,
			last_error=excluded.last_error,
			active=excluded.active,
			updated_at=excluded.updated_at
	`,
		rec.ID, acctType, rec.MicrosoftAccountID, rec.XboxUserID, rec.XboxGamertag,
		rec.MinecraftUUID, rec.MinecraftUsername, rec.ProfileName,
		rec.SkinURL, rec.SkinVariant, rec.CapeURL, rec.CapeAlias,
		rec.AuthStatus, rec.LicenseStatus,
		rec.LastAuthAt, rec.LastLicenseCheckAt, rec.ProfileUpdatedAt,
		rec.MSExpiresAt, rec.XBLExpiresAt, rec.XSTSExpiresAt, rec.MCExpiresAt,
		rec.VaultKey, rec.LastError, actInt, rec.CreatedAt, rec.UpdatedAt,
	)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (d *Database) ListAccounts() ([]domain.AccountRecord, error) {
	if d == nil {
		return nil, nil
	}
	rows, err := d.readDB.Query(`
		SELECT id, account_type, microsoft_account_id, xbox_user_id, xbox_gamertag,
		       minecraft_uuid, minecraft_username, profile_name,
		       skin_url, skin_variant, cape_url, cape_alias,
		       auth_status, license_status,
		       last_auth_at, last_license_check_at, profile_updated_at,
		       ms_expires_at, xbl_expires_at, xsts_expires_at, mc_expires_at,
		       vault_key, last_error, active, created_at, updated_at
		FROM accounts
		ORDER BY active DESC, updated_at DESC, created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.AccountRecord, 0, 8)
	for rows.Next() {
		var rec domain.AccountRecord
		var actInt int
		if rows.Scan(
			&rec.ID, &rec.AccountType, &rec.MicrosoftAccountID, &rec.XboxUserID, &rec.XboxGamertag,
			&rec.MinecraftUUID, &rec.MinecraftUsername, &rec.ProfileName,
			&rec.SkinURL, &rec.SkinVariant, &rec.CapeURL, &rec.CapeAlias,
			&rec.AuthStatus, &rec.LicenseStatus,
			&rec.LastAuthAt, &rec.LastLicenseCheckAt, &rec.ProfileUpdatedAt,
			&rec.MSExpiresAt, &rec.XBLExpiresAt, &rec.XSTSExpiresAt, &rec.MCExpiresAt,
			&rec.VaultKey, &rec.LastError, &actInt, &rec.CreatedAt, &rec.UpdatedAt,
		) == nil {
			rec.Active = actInt != 0
			if rec.AccountType == "" {
				rec.AccountType = "microsoft"
			}
			out = append(out, rec)
		}
	}
	return out, nil
}

func (d *Database) SetActiveAccount(id string) error {
	if d == nil || id == "" {
		return nil
	}
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	if d.closed.Load() {
		return nil
	}
	tx, err := d.writeDB.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE accounts SET active = 0`); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`UPDATE accounts SET active = 1 WHERE id = ?`, id); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (d *Database) DeleteAccount(id string) error {
	if d == nil || id == "" {
		return nil
	}
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	if d.closed.Load() {
		return nil
	}
	_, err := d.writeDB.Exec(`DELETE FROM accounts WHERE id = ?`, id)
	return err
}
