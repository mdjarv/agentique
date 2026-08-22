-- name: ListMachines :many
SELECT * FROM machines ORDER BY label;

-- name: UpsertMachine :exec
INSERT INTO machines (machine_id, label, base_url, token, added_at, icon)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(machine_id) DO UPDATE SET
  label = excluded.label,
  base_url = excluded.base_url,
  token = excluded.token,
  icon = excluded.icon;

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
