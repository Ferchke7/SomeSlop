-- name: ListTasks :many
SELECT id, title, status, priority, tags, due_at, estimate_seconds, project_id, goal_id, version
FROM focusflow.tasks
WHERE user_id = $1 AND archived_at IS NULL
ORDER BY (status = 'done'), due_at NULLS LAST, created_at DESC;

-- name: ListGoals :many
SELECT id, title, target_seconds, period_start, period_end, max_daily_seconds, session_seconds, color, version
FROM focusflow.goals
WHERE user_id = $1 AND archived_at IS NULL
ORDER BY period_start DESC;

