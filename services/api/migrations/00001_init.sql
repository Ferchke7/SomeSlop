-- +goose Up
CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS citext;
CREATE SCHEMA IF NOT EXISTS focusflow;

CREATE TABLE IF NOT EXISTS focusflow.users (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  email citext NOT NULL UNIQUE,
  display_name text NOT NULL,
  password_hash text NOT NULL,
  timezone text NOT NULL DEFAULT 'Asia/Tashkent',
  locale text NOT NULL DEFAULT 'ru',
  theme text NOT NULL DEFAULT 'system' CHECK (theme IN ('light', 'dark', 'system')),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  version integer NOT NULL DEFAULT 1
);

CREATE TABLE IF NOT EXISTS focusflow.sessions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES focusflow.users(id) ON DELETE CASCADE,
  token_hash bytea NOT NULL UNIQUE,
  expires_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  last_seen_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS sessions_user_idx ON focusflow.sessions(user_id, expires_at);

CREATE TABLE IF NOT EXISTS focusflow.projects (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES focusflow.users(id) ON DELETE CASCADE,
  name text NOT NULL,
  color text NOT NULL DEFAULT '#5b5bd6',
  icon text NOT NULL DEFAULT 'folder',
  archived_at timestamptz,
  position integer NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  version integer NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS projects_user_idx ON focusflow.projects(user_id, archived_at, position);

CREATE TABLE IF NOT EXISTS focusflow.goals (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES focusflow.users(id) ON DELETE CASCADE,
  project_id uuid REFERENCES focusflow.projects(id) ON DELETE SET NULL,
  title text NOT NULL,
  target_seconds bigint NOT NULL CHECK (target_seconds > 0),
  period_start date NOT NULL,
  period_end date NOT NULL,
  workdays smallint[] NOT NULL DEFAULT ARRAY[1,2,3,4,5,6,0]::smallint[],
  preferred_start time NOT NULL DEFAULT '09:00',
  preferred_end time NOT NULL DEFAULT '21:00',
  max_daily_seconds integer NOT NULL DEFAULT 28800,
  session_seconds integer NOT NULL DEFAULT 3000,
  color text NOT NULL DEFAULT '#5b5bd6',
  archived_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  version integer NOT NULL DEFAULT 1,
  CHECK (period_end >= period_start),
  CHECK (preferred_end > preferred_start)
);
CREATE INDEX IF NOT EXISTS goals_user_period_idx ON focusflow.goals(user_id, period_start, period_end);

CREATE TABLE IF NOT EXISTS focusflow.tasks (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES focusflow.users(id) ON DELETE CASCADE,
  project_id uuid REFERENCES focusflow.projects(id) ON DELETE SET NULL,
  goal_id uuid REFERENCES focusflow.goals(id) ON DELETE SET NULL,
  parent_id uuid REFERENCES focusflow.tasks(id) ON DELETE CASCADE,
  title text NOT NULL,
  description jsonb NOT NULL DEFAULT '[]'::jsonb,
  status text NOT NULL DEFAULT 'todo' CHECK (status IN ('todo', 'doing', 'done', 'cancelled')),
  priority smallint NOT NULL DEFAULT 3 CHECK (priority BETWEEN 1 AND 4),
  tags text[] NOT NULL DEFAULT '{}',
  due_at timestamptz,
  deadline_at timestamptz,
  estimate_seconds integer NOT NULL DEFAULT 0,
  recurrence_rule text,
  completed_at timestamptz,
  archived_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  version integer NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS tasks_user_status_due_idx ON focusflow.tasks(user_id, status, due_at);
CREATE INDEX IF NOT EXISTS tasks_parent_idx ON focusflow.tasks(parent_id);
CREATE INDEX IF NOT EXISTS tasks_tags_idx ON focusflow.tasks USING gin(tags);

CREATE TABLE IF NOT EXISTS focusflow.task_occurrences (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  task_id uuid NOT NULL REFERENCES focusflow.tasks(id) ON DELETE CASCADE,
  user_id uuid NOT NULL REFERENCES focusflow.users(id) ON DELETE CASCADE,
  occurrence_at timestamptz NOT NULL,
  completed_at timestamptz,
  skipped_at timestamptz,
  UNIQUE(task_id, occurrence_at)
);

CREATE TABLE IF NOT EXISTS focusflow.calendar_blocks (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES focusflow.users(id) ON DELETE CASCADE,
  task_id uuid REFERENCES focusflow.tasks(id) ON DELETE SET NULL,
  goal_id uuid REFERENCES focusflow.goals(id) ON DELETE SET NULL,
  title text NOT NULL,
  starts_at timestamptz NOT NULL,
  ends_at timestamptz NOT NULL,
  color text NOT NULL DEFAULT '#5b5bd6',
  source text NOT NULL DEFAULT 'manual' CHECK (source IN ('manual','planner','ics')),
  locked boolean NOT NULL DEFAULT false,
  completed_at timestamptz,
  external_uid text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  version integer NOT NULL DEFAULT 1,
  CHECK (ends_at > starts_at)
);
CREATE INDEX IF NOT EXISTS calendar_user_time_idx ON focusflow.calendar_blocks(user_id, starts_at, ends_at);
CREATE UNIQUE INDEX IF NOT EXISTS calendar_external_uid_idx ON focusflow.calendar_blocks(user_id, external_uid) WHERE external_uid IS NOT NULL;

CREATE TABLE IF NOT EXISTS focusflow.time_entries (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES focusflow.users(id) ON DELETE CASCADE,
  task_id uuid REFERENCES focusflow.tasks(id) ON DELETE SET NULL,
  project_id uuid REFERENCES focusflow.projects(id) ON DELETE SET NULL,
  goal_id uuid REFERENCES focusflow.goals(id) ON DELETE SET NULL,
  calendar_block_id uuid REFERENCES focusflow.calendar_blocks(id) ON DELETE SET NULL,
  started_at timestamptz NOT NULL,
  ended_at timestamptz NOT NULL,
  duration_seconds integer NOT NULL CHECK (duration_seconds >= 0),
  note text NOT NULL DEFAULT '',
  source text NOT NULL DEFAULT 'timer' CHECK (source IN ('timer','manual','import')),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  version integer NOT NULL DEFAULT 1,
  CHECK (ended_at >= started_at)
);
CREATE INDEX IF NOT EXISTS time_entries_user_time_idx ON focusflow.time_entries(user_id, started_at DESC);
CREATE INDEX IF NOT EXISTS time_entries_goal_idx ON focusflow.time_entries(goal_id, started_at);

CREATE TABLE IF NOT EXISTS focusflow.timer_state (
  user_id uuid PRIMARY KEY REFERENCES focusflow.users(id) ON DELETE CASCADE,
  task_id uuid REFERENCES focusflow.tasks(id) ON DELETE SET NULL,
  project_id uuid REFERENCES focusflow.projects(id) ON DELETE SET NULL,
  goal_id uuid REFERENCES focusflow.goals(id) ON DELETE SET NULL,
  calendar_block_id uuid REFERENCES focusflow.calendar_blocks(id) ON DELETE SET NULL,
  mode text NOT NULL DEFAULT 'stopwatch' CHECK (mode IN ('stopwatch','pomodoro')),
  status text NOT NULL DEFAULT 'idle' CHECK (status IN ('idle','running','paused')),
  started_at timestamptz,
  accumulated_seconds integer NOT NULL DEFAULT 0,
  target_seconds integer,
  updated_at timestamptz NOT NULL DEFAULT now(),
  version integer NOT NULL DEFAULT 1
);

CREATE TABLE IF NOT EXISTS focusflow.reminders (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES focusflow.users(id) ON DELETE CASCADE,
  task_id uuid REFERENCES focusflow.tasks(id) ON DELETE CASCADE,
  calendar_block_id uuid REFERENCES focusflow.calendar_blocks(id) ON DELETE CASCADE,
  title text NOT NULL,
  remind_at timestamptz NOT NULL,
  recurrence_rule text,
  status text NOT NULL DEFAULT 'scheduled' CHECK (status IN ('scheduled','sent','snoozed','cancelled')),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  version integer NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS reminders_due_idx ON focusflow.reminders(status, remind_at);

CREATE TABLE IF NOT EXISTS focusflow.reminder_deliveries (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  reminder_id uuid NOT NULL REFERENCES focusflow.reminders(id) ON DELETE CASCADE,
  channel text NOT NULL CHECK (channel IN ('in_app','push')),
  status text NOT NULL,
  idempotency_key text NOT NULL UNIQUE,
  error text,
  delivered_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS focusflow.notes (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES focusflow.users(id) ON DELETE CASCADE,
  parent_id uuid REFERENCES focusflow.notes(id) ON DELETE CASCADE,
  title text NOT NULL,
  content jsonb NOT NULL DEFAULT '[]'::jsonb,
  plain_text text NOT NULL DEFAULT '',
  icon text NOT NULL DEFAULT 'file-text',
  position integer NOT NULL DEFAULT 0,
  archived_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  version integer NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS notes_user_tree_idx ON focusflow.notes(user_id, parent_id, position);
CREATE INDEX IF NOT EXISTS notes_search_idx ON focusflow.notes USING gin(to_tsvector('simple', title || ' ' || plain_text));

CREATE TABLE IF NOT EXISTS focusflow.attachments (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES focusflow.users(id) ON DELETE CASCADE,
  note_id uuid REFERENCES focusflow.notes(id) ON DELETE CASCADE,
  file_name text NOT NULL,
  content_type text NOT NULL,
  size_bytes integer NOT NULL CHECK (size_bytes BETWEEN 0 AND 10485760),
  content bytea NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS focusflow.decks (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES focusflow.users(id) ON DELETE CASCADE,
  name text NOT NULL,
  color text NOT NULL DEFAULT '#2f9d74',
  new_per_day integer NOT NULL DEFAULT 20,
  reviews_per_day integer NOT NULL DEFAULT 200,
  desired_retention numeric(4,3) NOT NULL DEFAULT 0.900,
  archived_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  version integer NOT NULL DEFAULT 1
);

CREATE TABLE IF NOT EXISTS focusflow.cards (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES focusflow.users(id) ON DELETE CASCADE,
  deck_id uuid NOT NULL REFERENCES focusflow.decks(id) ON DELETE CASCADE,
  source_note_id uuid REFERENCES focusflow.notes(id) ON DELETE SET NULL,
  source_block_id text,
  card_type text NOT NULL DEFAULT 'basic' CHECK (card_type IN ('basic','cloze')),
  front text NOT NULL,
  back text NOT NULL DEFAULT '',
  tags text[] NOT NULL DEFAULT '{}',
  due_at timestamptz NOT NULL DEFAULT now(),
  fsrs_state jsonb NOT NULL DEFAULT '{}'::jsonb,
  suspended_at timestamptz,
  archived_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  version integer NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS cards_due_idx ON focusflow.cards(user_id, due_at) WHERE archived_at IS NULL AND suspended_at IS NULL;

CREATE TABLE IF NOT EXISTS focusflow.review_logs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES focusflow.users(id) ON DELETE CASCADE,
  card_id uuid NOT NULL REFERENCES focusflow.cards(id) ON DELETE CASCADE,
  rating smallint NOT NULL CHECK (rating BETWEEN 1 AND 4),
  reviewed_at timestamptz NOT NULL,
  duration_ms integer NOT NULL DEFAULT 0,
  previous_state jsonb NOT NULL,
  next_state jsonb NOT NULL,
  scheduled_days integer NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS review_logs_card_idx ON focusflow.review_logs(card_id, reviewed_at DESC);

CREATE TABLE IF NOT EXISTS focusflow.anki_links (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES focusflow.users(id) ON DELETE CASCADE,
  card_id uuid NOT NULL REFERENCES focusflow.cards(id) ON DELETE CASCADE,
  anki_note_id bigint NOT NULL,
  anki_deck text NOT NULL,
  anki_model text NOT NULL,
  field_mapping jsonb NOT NULL DEFAULT '{}'::jsonb,
  last_local_hash text,
  last_anki_hash text,
  last_synced_at timestamptz,
  conflict jsonb,
  archived_at timestamptz,
  UNIQUE(user_id, anki_note_id),
  UNIQUE(card_id)
);

CREATE TABLE IF NOT EXISTS focusflow.anki_sync_runs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES focusflow.users(id) ON DELETE CASCADE,
  status text NOT NULL,
  imported integer NOT NULL DEFAULT 0,
  exported integer NOT NULL DEFAULT 0,
  conflicts integer NOT NULL DEFAULT 0,
  error text,
  started_at timestamptz NOT NULL DEFAULT now(),
  finished_at timestamptz
);

CREATE TABLE IF NOT EXISTS focusflow.push_subscriptions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES focusflow.users(id) ON DELETE CASCADE,
  endpoint text NOT NULL UNIQUE,
  p256dh text NOT NULL,
  auth text NOT NULL,
  user_agent text,
  created_at timestamptz NOT NULL DEFAULT now(),
  last_used_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS focusflow.background_jobs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  kind text NOT NULL,
  payload jsonb NOT NULL DEFAULT '{}'::jsonb,
  run_at timestamptz NOT NULL DEFAULT now(),
  status text NOT NULL DEFAULT 'available' CHECK (status IN ('available','running','completed','failed')),
  attempts integer NOT NULL DEFAULT 0,
  max_attempts integer NOT NULL DEFAULT 10,
  idempotency_key text UNIQUE,
  locked_at timestamptz,
  locked_by text,
  last_error text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS jobs_available_idx ON focusflow.background_jobs(status, run_at);

CREATE TABLE IF NOT EXISTS focusflow.outbox (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  topic text NOT NULL,
  aggregate_id uuid,
  payload jsonb NOT NULL,
  published_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS outbox_unpublished_idx ON focusflow.outbox(created_at) WHERE published_at IS NULL;

-- +goose Down
DROP SCHEMA IF EXISTS focusflow CASCADE;
