-- name: ListMachines :many
SELECT * FROM machines ORDER BY label;

-- name: UpsertMachine :exec
-- platform_os keeps its stored value when the caller sends empty: a client
-- that predates the field re-upserts rows on re-pair, and blanking a known
-- platform would strip the glyph until the next fresh pair.
INSERT INTO machines (machine_id, label, base_url, token, added_at, icon, session_id, identity_key, platform_os)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(machine_id) DO UPDATE SET
  label = excluded.label,
  base_url = excluded.base_url,
  token = excluded.token,
  icon = excluded.icon,
  session_id = excluded.session_id,
  identity_key = excluded.identity_key,
  platform_os = CASE WHEN excluded.platform_os = '' THEN machines.platform_os ELSE excluded.platform_os END;

-- name: GetMachine :one
SELECT * FROM machines WHERE machine_id = ?;

-- name: UpdateMachinePresentation :execrows
UPDATE machines SET label = ?, icon = ? WHERE machine_id = ?;

-- name: DeleteMachine :exec
DELETE FROM machines WHERE machine_id = ?;

-- name: GetHostPresentation :one
SELECT label, icon FROM host_presentation WHERE id = 1;

-- name: SetHostPresentation :exec
INSERT INTO host_presentation (id, label, icon)
VALUES (1, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  label = excluded.label,
  icon = excluded.icon;
