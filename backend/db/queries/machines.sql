-- name: ListMachines :many
SELECT * FROM machines ORDER BY label;

-- name: UpsertMachine :exec
INSERT INTO machines (machine_id, label, base_url, token, added_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(machine_id) DO UPDATE SET
  label = excluded.label,
  base_url = excluded.base_url,
  token = excluded.token;

-- name: DeleteMachine :exec
DELETE FROM machines WHERE machine_id = ?;
