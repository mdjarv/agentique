-- name: ListRecentActivityByProject :many
SELECT
  'message' AS kind,
  m.id AS item_id,
  m.channel_id AS source_id,
  m.sender_name AS source_name,
  SUBSTR(m.content, 1, 200) AS content,
  m.message_type AS event_type,
  '' AS category,
  '' AS file_path,
  m.created_at
FROM messages m
JOIN channels c ON c.id = m.channel_id
WHERE c.project_id = @project_id
  AND m.created_at >= @since

UNION ALL

SELECT
  'event' AS kind,
  CAST(e.id AS TEXT) AS item_id,
  e.session_id AS source_id,
  s.name AS source_name,
  CASE e.type
    WHEN 'tool_use' THEN COALESCE(json_extract(e.data, '$.toolName'), '')
    WHEN 'error' THEN COALESCE(SUBSTR(json_extract(e.data, '$.content'), 1, 200), '')
    ELSE ''
  END AS content,
  e.type AS event_type,
  CASE e.type
    WHEN 'tool_use' THEN COALESCE(json_extract(e.data, '$.category'), '')
    ELSE ''
  END AS category,
  CASE e.type
    WHEN 'tool_use' THEN COALESCE(
      json_extract(e.data, '$.toolInput.file_path'),
      json_extract(e.data, '$.toolInput.path'),
      ''
    )
    ELSE ''
  END AS file_path,
  e.created_at
FROM session_events e
JOIN sessions s ON s.id = e.session_id
WHERE s.project_id = @project_id
  AND e.type IN ('tool_use', 'result', 'error')
  AND e.created_at >= @since

ORDER BY created_at DESC
LIMIT 100;

-- name: ListRecentActivityGlobal :many
SELECT
  'message' AS kind,
  m.id AS item_id,
  m.channel_id AS source_id,
  m.sender_name AS source_name,
  SUBSTR(m.content, 1, 200) AS content,
  m.message_type AS event_type,
  '' AS category,
  '' AS file_path,
  COALESCE(p.slug, '') AS project_slug,
  -- Explicit alias: with a LEFT JOIN in the compound's first SELECT, SQLite
  -- (modernc build) drops the short output name and the trailing ORDER BY
  -- can no longer resolve a bare created_at.
  m.created_at AS created_at
FROM messages m
JOIN channels c ON c.id = m.channel_id
LEFT JOIN projects p ON p.id = c.project_id
WHERE m.created_at >= @since

UNION ALL

SELECT
  'event' AS kind,
  CAST(e.id AS TEXT) AS item_id,
  e.session_id AS source_id,
  s.name AS source_name,
  CASE e.type
    WHEN 'tool_use' THEN COALESCE(json_extract(e.data, '$.toolName'), '')
    WHEN 'error' THEN COALESCE(SUBSTR(json_extract(e.data, '$.content'), 1, 200), '')
    ELSE ''
  END AS content,
  e.type AS event_type,
  CASE e.type
    WHEN 'tool_use' THEN COALESCE(json_extract(e.data, '$.category'), '')
    ELSE ''
  END AS category,
  CASE e.type
    WHEN 'tool_use' THEN COALESCE(
      json_extract(e.data, '$.toolInput.file_path'),
      json_extract(e.data, '$.toolInput.path'),
      ''
    )
    ELSE ''
  END AS file_path,
  p.slug AS project_slug,
  e.created_at AS created_at
FROM session_events e
JOIN sessions s ON s.id = e.session_id
JOIN projects p ON p.id = s.project_id
WHERE e.type IN ('tool_use', 'result', 'error')
  AND e.created_at >= @since

ORDER BY created_at DESC
LIMIT @lim;
