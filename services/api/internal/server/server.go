package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	fsrs "github.com/open-spaced-repetition/go-fsrs/v4"

	"focusflow/services/api/internal/auth"
	"focusflow/services/api/internal/planner"
)

type Config struct {
	Addr            string
	DatabaseURL     string
	AppOrigin       string
	Environment     string
	SessionTTL      time.Duration
	StaticDir       string
	VAPIDPublicKey  string
	VAPIDPrivateKey string
	VAPIDSubject    string
}

type Server struct {
	cfg    Config
	db     *pgxpool.Pool
	router chi.Router
	log    *slog.Logger
}

type contextKey string

const userKey contextKey = "focusflow-user"

type User struct {
	ID          uuid.UUID `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"displayName"`
	Timezone    string    `json:"timezone"`
	Locale      string    `json:"locale"`
	Theme       string    `json:"theme"`
	Version     int       `json:"version"`
}

type APIError struct {
	Code        string            `json:"code"`
	Message     string            `json:"message"`
	FieldErrors map[string]string `json:"fieldErrors,omitempty"`
	RequestID   string            `json:"requestId,omitempty"`
}

func New(ctx context.Context, cfg Config, logger *slog.Logger) (*Server, error) {
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	s := &Server{cfg: cfg, db: pool, log: logger}
	s.router = s.routes()
	return s, nil
}

func (s *Server) Close()                { s.db.Close() }
func (s *Server) Handler() http.Handler { return s.router }

func (s *Server) routes() chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.RealIP, middleware.Recoverer)
	r.Use(s.logRequests)
	r.Use(s.securityHeaders)
	r.Get("/health", s.health)
	r.Route("/api/v1", func(api chi.Router) {
		api.Get("/meta", s.meta)
		api.Route("/auth", func(ar chi.Router) {
			ar.Post("/register", s.register)
			ar.Post("/login", s.login)
			ar.Post("/logout", s.logout)
			ar.Post("/demo", s.demo)
		})
		api.Group(func(protected chi.Router) {
			protected.Use(s.requireUser)
			protected.Get("/me", s.me)
			protected.Get("/dashboard", s.dashboard)
			protected.Route("/tasks", func(tr chi.Router) {
				tr.Get("/", s.listTasks)
				tr.Post("/", s.createTask)
				tr.Patch("/{id}", s.patchTask)
				tr.Delete("/{id}", s.archiveTask)
			})
			protected.Route("/goals", func(gr chi.Router) {
				gr.Get("/", s.listGoals)
				gr.Post("/", s.createGoal)
				gr.Post("/{id}/plan", s.planGoal)
			})
			protected.Route("/calendar", func(cr chi.Router) {
				cr.Get("/", s.listCalendar)
				cr.Post("/", s.createCalendarBlock)
				cr.Patch("/{id}", s.patchCalendarBlock)
				cr.Delete("/{id}", s.deleteCalendarBlock)
				cr.Get("/export.ics", s.exportICS)
				cr.Post("/import.ics", s.importICS)
			})
			protected.Route("/timer", func(tr chi.Router) {
				tr.Get("/", s.getTimer)
				tr.Post("/start", s.startTimer)
				tr.Post("/pause", s.pauseTimer)
				tr.Post("/resume", s.resumeTimer)
				tr.Post("/stop", s.stopTimer)
			})
			protected.Route("/notes", func(nr chi.Router) {
				nr.Get("/", s.listNotes)
				nr.Post("/", s.createNote)
				nr.Patch("/{id}", s.patchNote)
				nr.Delete("/{id}", s.archiveNote)
				nr.Post("/{id}/attachments", s.uploadAttachment)
			})
			protected.Get("/attachments/{id}", s.getAttachment)
			protected.Route("/decks", func(dr chi.Router) {
				dr.Get("/", s.listDecks)
				dr.Post("/", s.createDeck)
			})
			protected.Route("/cards", func(cr chi.Router) {
				cr.Get("/", s.listCards)
				cr.Get("/due", s.dueCards)
				cr.Post("/", s.createCard)
				cr.Post("/{id}/review", s.reviewCard)
			})
			protected.Route("/anki", func(ar chi.Router) {
				ar.Get("/export", s.ankiExport)
				ar.Post("/import", s.ankiImport)
				ar.Post("/link", s.ankiLink)
				ar.Get("/status", s.ankiStatus)
			})
			protected.Route("/notifications", func(nr chi.Router) {
				nr.Get("/", s.listNotifications)
				nr.Post("/{id}/snooze", s.snoozeNotification)
			})
			protected.Post("/push/subscriptions", s.savePushSubscription)
			protected.Get("/analytics", s.analytics)
		})
	})
	if s.cfg.StaticDir != "" {
		r.Handle("/*", s.staticHandler())
	}
	return r
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		s.log.Info("http_request", "method", r.Method, "path", r.URL.Path, "status", ww.Status(), "duration_ms", time.Since(start).Milliseconds(), "request_id", middleware.GetReqID(r.Context()))
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		if origin := r.Header.Get("Origin"); origin != "" && s.cfg.AppOrigin != "" {
			if origin == s.cfg.AppOrigin {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Requested-With")
				w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PATCH,DELETE,OPTIONS")
			} else if r.Method != http.MethodGet && r.Method != http.MethodHead {
				writeError(w, r, http.StatusForbidden, "origin_not_allowed", "Источник запроса не разрешён", nil)
				return
			}
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) staticHandler() http.Handler {
	fs := http.FileServer(http.Dir(s.cfg.StaticDir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Join(s.cfg.StaticDir, filepath.Clean(r.URL.Path))
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			fs.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(s.cfg.StaticDir, "index.html"))
	})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.db.Ping(ctx); err != nil {
		writeError(w, r, http.StatusServiceUnavailable, "database_unavailable", "База данных недоступна", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "time": time.Now().UTC()})
}

func (s *Server) meta(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"name": "FocusFlow", "version": "0.1.0",
		"vapidPublicKey": s.cfg.VAPIDPublicKey,
		"features":       []string{"tasks", "goals", "calendar", "timer", "notes", "fsrs", "anki", "pwa"},
	})
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email    string `json:"email"`
		Name     string `json:"name"`
		Password string `json:"password"`
		Timezone string `json:"timezone"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		input.Name = strings.Split(input.Email, "@")[0]
	}
	if !strings.Contains(input.Email, "@") {
		writeError(w, r, http.StatusUnprocessableEntity, "validation_error", "Проверьте email", map[string]string{"email": "Введите корректный email"})
		return
	}
	if input.Timezone == "" {
		input.Timezone = "Asia/Tashkent"
	}
	hash, err := auth.HashPassword(input.Password)
	if err != nil {
		writeError(w, r, http.StatusUnprocessableEntity, "validation_error", "Пароль слишком короткий", map[string]string{"password": "Минимум 8 символов"})
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	var user User
	err = tx.QueryRow(r.Context(), `
		INSERT INTO focusflow.users(email,display_name,password_hash,timezone)
		VALUES($1,$2,$3,$4)
		RETURNING id,email,display_name,timezone,locale,theme,version`,
		input.Email, input.Name, hash, input.Timezone,
	).Scan(&user.ID, &user.Email, &user.DisplayName, &user.Timezone, &user.Locale, &user.Theme, &user.Version)
	if err != nil {
		if strings.Contains(err.Error(), "users_email_key") {
			writeError(w, r, http.StatusConflict, "email_exists", "Аккаунт с таким email уже существует", nil)
		} else {
			writeDBError(w, r, err)
		}
		return
	}
	if err := seedWorkspace(r.Context(), tx, user.ID); err != nil {
		writeDBError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeDBError(w, r, err)
		return
	}
	if err := s.issueSession(w, r, user.ID); err != nil {
		writeDBError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, user)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var input struct{ Email, Password string }
	if !decodeJSON(w, r, &input) {
		return
	}
	var user User
	var hash string
	err := s.db.QueryRow(r.Context(), `
		SELECT id,email,display_name,password_hash,timezone,locale,theme,version
		FROM focusflow.users WHERE email=$1`, strings.ToLower(strings.TrimSpace(input.Email)),
	).Scan(&user.ID, &user.Email, &user.DisplayName, &hash, &user.Timezone, &user.Locale, &user.Theme, &user.Version)
	if err != nil || !auth.VerifyPassword(hash, input.Password) {
		writeError(w, r, http.StatusUnauthorized, "invalid_credentials", "Неверный email или пароль", nil)
		return
	}
	if err := s.issueSession(w, r, user.ID); err != nil {
		writeDBError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (s *Server) demo(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Environment == "production" {
		writeError(w, r, http.StatusNotFound, "not_found", "Маршрут не найден", nil)
		return
	}
	email := fmt.Sprintf("demo-%d@focusflow.local", time.Now().UnixNano())
	hash, _ := auth.HashPassword(base64.RawURLEncoding.EncodeToString(randomBytes(12)))
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	var user User
	err = tx.QueryRow(r.Context(), `
		INSERT INTO focusflow.users(email,display_name,password_hash,timezone)
		VALUES($1,'Фердаус',$2,'Asia/Tashkent')
		RETURNING id,email,display_name,timezone,locale,theme,version`, email, hash,
	).Scan(&user.ID, &user.Email, &user.DisplayName, &user.Timezone, &user.Locale, &user.Theme, &user.Version)
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	if err := seedWorkspace(r.Context(), tx, user.ID); err != nil {
		writeDBError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeDBError(w, r, err)
		return
	}
	if err := s.issueSession(w, r, user.ID); err != nil {
		writeDBError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, user)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("focusflow_session"); err == nil {
		sum := sha256.Sum256([]byte(cookie.Value))
		_, _ = s.db.Exec(r.Context(), `DELETE FROM focusflow.sessions WHERE token_hash=$1`, sum[:])
	}
	http.SetCookie(w, &http.Cookie{Name: "focusflow_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: s.cfg.Environment == "production"})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) issueSession(w http.ResponseWriter, r *http.Request, userID uuid.UUID) error {
	token := base64.RawURLEncoding.EncodeToString(randomBytes(32))
	sum := sha256.Sum256([]byte(token))
	expires := time.Now().Add(s.cfg.SessionTTL)
	if _, err := s.db.Exec(r.Context(), `INSERT INTO focusflow.sessions(user_id,token_hash,expires_at) VALUES($1,$2,$3)`, userID, sum[:], expires); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{Name: "focusflow_session", Value: token, Path: "/", Expires: expires, MaxAge: int(s.cfg.SessionTTL.Seconds()), HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: s.cfg.Environment == "production"})
	return nil
}

func (s *Server) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("focusflow_session")
		if err != nil {
			writeError(w, r, http.StatusUnauthorized, "authentication_required", "Войдите в аккаунт", nil)
			return
		}
		sum := sha256.Sum256([]byte(cookie.Value))
		var user User
		err = s.db.QueryRow(r.Context(), `
			SELECT u.id,u.email,u.display_name,u.timezone,u.locale,u.theme,u.version
			FROM focusflow.sessions s JOIN focusflow.users u ON u.id=s.user_id
			WHERE s.token_hash=$1 AND s.expires_at>now()`, sum[:],
		).Scan(&user.ID, &user.Email, &user.DisplayName, &user.Timezone, &user.Locale, &user.Theme, &user.Version)
		if err != nil {
			writeError(w, r, http.StatusUnauthorized, "session_expired", "Сессия истекла", nil)
			return
		}
		_, _ = s.db.Exec(r.Context(), `UPDATE focusflow.sessions SET last_seen_at=now() WHERE token_hash=$1 AND last_seen_at<now()-interval '5 minutes'`, sum[:])
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey, user)))
	})
}

func currentUser(r *http.Request) User { return r.Context().Value(userKey).(User) }
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, currentUser(r))
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var taskOpen, taskDone, dueCards, notes int
	var trackedToday int64
	_ = s.db.QueryRow(r.Context(), `
		SELECT
		  count(*) FILTER (WHERE status IN ('todo','doing')),
		  count(*) FILTER (WHERE status='done' AND completed_at::date=(now() AT TIME ZONE $2)::date)
		FROM focusflow.tasks WHERE user_id=$1 AND archived_at IS NULL`, u.ID, u.Timezone).Scan(&taskOpen, &taskDone)
	_ = s.db.QueryRow(r.Context(), `SELECT count(*) FROM focusflow.cards WHERE user_id=$1 AND archived_at IS NULL AND suspended_at IS NULL AND due_at<=now()`, u.ID).Scan(&dueCards)
	_ = s.db.QueryRow(r.Context(), `SELECT count(*) FROM focusflow.notes WHERE user_id=$1 AND archived_at IS NULL`, u.ID).Scan(&notes)
	_ = s.db.QueryRow(r.Context(), `SELECT coalesce(sum(duration_seconds),0) FROM focusflow.time_entries WHERE user_id=$1 AND started_at::date=(now() AT TIME ZONE $2)::date`, u.ID, u.Timezone).Scan(&trackedToday)
	writeJSON(w, http.StatusOK, map[string]any{
		"openTasks": taskOpen, "completedToday": taskDone, "dueCards": dueCards, "notes": notes, "trackedTodaySeconds": trackedToday,
		"greeting": greetingFor(time.Now(), u.Timezone),
	})
}

func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	rows, err := s.db.Query(r.Context(), `
		SELECT t.id,t.title,t.status,t.priority,t.tags,t.due_at,t.deadline_at,t.estimate_seconds,t.project_id,t.goal_id,
		       t.parent_id,t.recurrence_rule,t.completed_at,t.version,p.name,p.color
		FROM focusflow.tasks t LEFT JOIN focusflow.projects p ON p.id=t.project_id
		WHERE t.user_id=$1 AND t.archived_at IS NULL
		ORDER BY (t.status IN ('done','cancelled')), t.due_at NULLS LAST, t.priority, t.created_at DESC`, u.ID)
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id uuid.UUID
		var title, status string
		var priority int16
		var tags []string
		var due, deadline, completed *time.Time
		var estimate int
		var projectID, goalID, parentID *uuid.UUID
		var recurrence *string
		var version int
		var projectName, projectColor *string
		if err := rows.Scan(&id, &title, &status, &priority, &tags, &due, &deadline, &estimate, &projectID, &goalID, &parentID, &recurrence, &completed, &version, &projectName, &projectColor); err != nil {
			writeDBError(w, r, err)
			return
		}
		items = append(items, map[string]any{"id": id, "title": title, "status": status, "priority": priority, "tags": tags, "dueAt": due, "deadlineAt": deadline, "estimateSeconds": estimate, "projectId": projectID, "goalId": goalID, "parentId": parentID, "recurrenceRule": recurrence, "completedAt": completed, "version": version, "projectName": projectName, "projectColor": projectColor})
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var in struct {
		Title           string     `json:"title"`
		ProjectID       *uuid.UUID `json:"projectId"`
		GoalID          *uuid.UUID `json:"goalId"`
		ParentID        *uuid.UUID `json:"parentId"`
		Priority        int16      `json:"priority"`
		Tags            []string   `json:"tags"`
		DueAt           *time.Time `json:"dueAt"`
		EstimateSeconds int        `json:"estimateSeconds"`
		RecurrenceRule  *string    `json:"recurrenceRule"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		writeError(w, r, 422, "validation_error", "Введите название задачи", map[string]string{"title": "Обязательное поле"})
		return
	}
	if in.Priority == 0 {
		in.Priority = 3
	}
	var item map[string]any
	var id uuid.UUID
	var created time.Time
	err := s.db.QueryRow(r.Context(), `
		INSERT INTO focusflow.tasks(user_id,title,project_id,goal_id,parent_id,priority,tags,due_at,estimate_seconds,recurrence_rule)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id,created_at`,
		u.ID, in.Title, in.ProjectID, in.GoalID, in.ParentID, in.Priority, in.Tags, in.DueAt, in.EstimateSeconds, in.RecurrenceRule).Scan(&id, &created)
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	item = map[string]any{"id": id, "title": in.Title, "status": "todo", "priority": in.Priority, "tags": in.Tags, "dueAt": in.DueAt, "estimateSeconds": in.EstimateSeconds, "projectId": in.ProjectID, "goalId": in.GoalID, "version": 1, "createdAt": created}
	if in.DueAt != nil {
		remindAt := in.DueAt.Add(-15 * time.Minute)
		if remindAt.Before(time.Now()) {
			remindAt = *in.DueAt
		}
		_, _ = s.db.Exec(r.Context(), `INSERT INTO focusflow.reminders(user_id,task_id,title,remind_at) VALUES($1,$2,$3,$4)`, u.ID, id, in.Title, remindAt)
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) patchTask(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, 400, "invalid_id", "Некорректный идентификатор", nil)
		return
	}
	var in struct {
		Title           *string    `json:"title"`
		Status          *string    `json:"status"`
		Priority        *int16     `json:"priority"`
		DueAt           *time.Time `json:"dueAt"`
		EstimateSeconds *int       `json:"estimateSeconds"`
		Version         int        `json:"version"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	var version int
	err = s.db.QueryRow(r.Context(), `
		UPDATE focusflow.tasks SET
		  title=coalesce($3,title), status=coalesce($4,status), priority=coalesce($5,priority),
		  due_at=CASE WHEN $6::boolean THEN $7 ELSE due_at END,
		  estimate_seconds=coalesce($8,estimate_seconds),
		  completed_at=CASE WHEN coalesce($4,status)='done' THEN coalesce(completed_at,now()) ELSE NULL END,
		  updated_at=now(),version=version+1
		WHERE id=$1 AND user_id=$2 AND version=$9 AND archived_at IS NULL RETURNING version`,
		id, u.ID, in.Title, in.Status, in.Priority, in.DueAt != nil, in.DueAt, in.EstimateSeconds, in.Version).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, 409, "version_conflict", "Задача уже изменена на другом устройстве", nil)
		return
	}
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "version": version})
}

func (s *Server) archiveTask(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, 400, "invalid_id", "Некорректный идентификатор", nil)
		return
	}
	tag, err := s.db.Exec(r.Context(), `UPDATE focusflow.tasks SET archived_at=now(),updated_at=now(),version=version+1 WHERE id=$1 AND user_id=$2`, id, u.ID)
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, r, 404, "not_found", "Задача не найдена", nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type goalResponse struct {
	ID              uuid.UUID        `json:"id"`
	Title           string           `json:"title"`
	TargetSeconds   int64            `json:"targetSeconds"`
	PeriodStart     time.Time        `json:"periodStart"`
	PeriodEnd       time.Time        `json:"periodEnd"`
	MaxDailySeconds int              `json:"maxDailySeconds"`
	SessionSeconds  int              `json:"sessionSeconds"`
	Color           string           `json:"color"`
	Workdays        []int16          `json:"workdays"`
	PreferredStart  string           `json:"preferredStart"`
	PreferredEnd    string           `json:"preferredEnd"`
	Version         int              `json:"version"`
	Forecast        planner.Forecast `json:"forecast"`
}

func (s *Server) listGoals(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	rows, err := s.db.Query(r.Context(), `
		SELECT id,title,target_seconds,period_start,period_end,max_daily_seconds,session_seconds,color,workdays,
		       to_char(preferred_start,'HH24:MI'),to_char(preferred_end,'HH24:MI'),version
		FROM focusflow.goals WHERE user_id=$1 AND archived_at IS NULL ORDER BY period_end DESC`, u.ID)
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]goalResponse, 0)
	for rows.Next() {
		var g goalResponse
		if err := rows.Scan(&g.ID, &g.Title, &g.TargetSeconds, &g.PeriodStart, &g.PeriodEnd, &g.MaxDailySeconds, &g.SessionSeconds, &g.Color, &g.Workdays, &g.PreferredStart, &g.PreferredEnd, &g.Version); err != nil {
			writeDBError(w, r, err)
			return
		}
		g.Forecast = s.goalForecast(r.Context(), u, g)
		items = append(items, g)
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) goalForecast(ctx context.Context, u User, g goalResponse) planner.Forecast {
	var tracked, planned int64
	_ = s.db.QueryRow(ctx, `SELECT coalesce(sum(duration_seconds),0) FROM focusflow.time_entries WHERE user_id=$1 AND goal_id=$2 AND started_at::date BETWEEN $3 AND $4`, u.ID, g.ID, g.PeriodStart, g.PeriodEnd).Scan(&tracked)
	_ = s.db.QueryRow(ctx, `SELECT coalesce(sum(extract(epoch from ends_at-starts_at)),0)::bigint FROM focusflow.calendar_blocks WHERE user_id=$1 AND goal_id=$2 AND starts_at>now()`, u.ID, g.ID).Scan(&planned)
	days := goalDays(u.Timezone, g, time.Now())
	if occupiedDays, _, err := calendarCapacity(ctx, s.db, u.ID, g, days, u.Timezone); err == nil {
		days = occupiedDays
	}
	return planner.Calculate(g.TargetSeconds, tracked, planned, days)
}

func (s *Server) createGoal(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var in struct {
		Title          string  `json:"title"`
		TargetHours    float64 `json:"targetHours"`
		PeriodStart    string  `json:"periodStart"`
		PeriodEnd      string  `json:"periodEnd"`
		MaxDailyHours  float64 `json:"maxDailyHours"`
		SessionMinutes int     `json:"sessionMinutes"`
		Workdays       []int16 `json:"workdays"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	in.Title = strings.TrimSpace(in.Title)
	start, e1 := time.Parse("2006-01-02", in.PeriodStart)
	end, e2 := time.Parse("2006-01-02", in.PeriodEnd)
	if in.Title == "" || in.TargetHours <= 0 || e1 != nil || e2 != nil || end.Before(start) {
		writeError(w, r, 422, "validation_error", "Проверьте параметры цели", nil)
		return
	}
	if in.MaxDailyHours <= 0 {
		in.MaxDailyHours = 8
	}
	if in.SessionMinutes <= 0 {
		in.SessionMinutes = 50
	}
	if len(in.Workdays) == 0 {
		in.Workdays = []int16{1, 2, 3, 4, 5, 6, 0}
	}
	var id uuid.UUID
	err := s.db.QueryRow(r.Context(), `INSERT INTO focusflow.goals(user_id,title,target_seconds,period_start,period_end,max_daily_seconds,session_seconds,workdays) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`, u.ID, in.Title, int64(math.Round(in.TargetHours*3600)), start, end, int(math.Round(in.MaxDailyHours*3600)), in.SessionMinutes*60, in.Workdays).Scan(&id)
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "version": 1})
}

func (s *Server) planGoal(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, 400, "invalid_id", "Некорректная цель", nil)
		return
	}
	var g goalResponse
	err = s.db.QueryRow(r.Context(), `SELECT id,title,target_seconds,period_start,period_end,max_daily_seconds,session_seconds,color,workdays,to_char(preferred_start,'HH24:MI'),to_char(preferred_end,'HH24:MI'),version FROM focusflow.goals WHERE id=$1 AND user_id=$2 AND archived_at IS NULL`, id, u.ID).Scan(&g.ID, &g.Title, &g.TargetSeconds, &g.PeriodStart, &g.PeriodEnd, &g.MaxDailySeconds, &g.SessionSeconds, &g.Color, &g.Workdays, &g.PreferredStart, &g.PreferredEnd, &g.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, 404, "not_found", "Цель не найдена", nil)
		return
	}
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	_, err = tx.Exec(r.Context(), `DELETE FROM focusflow.calendar_blocks WHERE user_id=$1 AND goal_id=$2 AND source='planner' AND locked=false AND starts_at>now()`, u.ID, g.ID)
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	var tracked int64
	_ = tx.QueryRow(r.Context(), `SELECT coalesce(sum(duration_seconds),0) FROM focusflow.time_entries WHERE user_id=$1 AND goal_id=$2 AND started_at::date BETWEEN $3 AND $4`, u.ID, g.ID, g.PeriodStart, g.PeriodEnd).Scan(&tracked)
	var planned int64
	_ = tx.QueryRow(r.Context(), `SELECT coalesce(sum(extract(epoch from ends_at-starts_at)),0)::bigint FROM focusflow.calendar_blocks WHERE user_id=$1 AND goal_id=$2 AND starts_at>now()`, u.ID, g.ID).Scan(&planned)
	days := goalDays(u.Timezone, g, time.Now())
	days, occupied, err := calendarCapacity(r.Context(), tx, u.ID, g, days, u.Timezone)
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	remaining := max64(g.TargetSeconds-tracked-planned, 0)
	alloc, allocErr := planner.Distribute(remaining, days, int64(g.SessionSeconds))
	if allocErr != nil && len(alloc) == 0 {
		tx.Rollback(r.Context())
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"forecast": planner.Calculate(g.TargetSeconds, tracked, planned, days), "message": "В календаре недостаточно свободного времени"})
		return
	}
	loc, _ := time.LoadLocation(u.Timezone)
	if loc == nil {
		loc = time.UTC
	}
	created := 0
	partial := allocErr != nil
	for _, a := range alloc {
		left := a.Seconds
		for left > 0 {
			chunk := min64(int64(g.SessionSeconds), left)
			windowStart, windowEnd := goalWindow(a.Date, g, loc)
			cursor, ok := firstFreeSlot(windowStart, windowEnd, time.Duration(chunk)*time.Second, occupied[dayKey(a.Date)])
			if !ok {
				partial = true
				break
			}
			end := cursor.Add(time.Duration(chunk) * time.Second)
			_, err = tx.Exec(r.Context(), `INSERT INTO focusflow.calendar_blocks(user_id,goal_id,title,starts_at,ends_at,color,source) VALUES($1,$2,$3,$4,$5,$6,'planner')`, u.ID, g.ID, g.Title, cursor, end, g.Color)
			if err != nil {
				writeDBError(w, r, err)
				return
			}
			occupied[dayKey(a.Date)] = append(occupied[dayKey(a.Date)], timeSpan{Start: cursor, End: end})
			created++
			left -= chunk
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeDBError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"created": created, "partial": partial})
}

func (s *Server) listCalendar(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	from := time.Now().AddDate(0, -1, 0)
	to := time.Now().AddDate(0, 2, 0)
	if v := r.URL.Query().Get("from"); v != "" {
		if t, e := time.Parse(time.RFC3339, v); e == nil {
			from = t
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if t, e := time.Parse(time.RFC3339, v); e == nil {
			to = t
		}
	}
	rows, err := s.db.Query(r.Context(), `SELECT id,title,starts_at,ends_at,color,source,locked,completed_at,task_id,goal_id,version FROM focusflow.calendar_blocks WHERE user_id=$1 AND starts_at<$3 AND ends_at>$2 ORDER BY starts_at`, u.ID, from, to)
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id uuid.UUID
		var title, color, source string
		var start, end time.Time
		var locked bool
		var completed *time.Time
		var taskID, goalID *uuid.UUID
		var version int
		if err := rows.Scan(&id, &title, &start, &end, &color, &source, &locked, &completed, &taskID, &goalID, &version); err != nil {
			writeDBError(w, r, err)
			return
		}
		items = append(items, map[string]any{"id": id, "title": title, "startsAt": start, "endsAt": end, "color": color, "source": source, "locked": locked, "completedAt": completed, "taskId": taskID, "goalId": goalID, "version": version})
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) createCalendarBlock(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var in struct {
		Title    string     `json:"title"`
		StartsAt time.Time  `json:"startsAt"`
		EndsAt   time.Time  `json:"endsAt"`
		Color    string     `json:"color"`
		TaskID   *uuid.UUID `json:"taskId"`
		GoalID   *uuid.UUID `json:"goalId"`
		Locked   bool       `json:"locked"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Title == "" || !in.EndsAt.After(in.StartsAt) {
		writeError(w, r, 422, "validation_error", "Проверьте время блока", nil)
		return
	}
	if in.Color == "" {
		in.Color = "#5b5bd6"
	}
	var id uuid.UUID
	err := s.db.QueryRow(r.Context(), `INSERT INTO focusflow.calendar_blocks(user_id,task_id,goal_id,title,starts_at,ends_at,color,locked) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`, u.ID, in.TaskID, in.GoalID, in.Title, in.StartsAt, in.EndsAt, in.Color, in.Locked).Scan(&id)
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	remindAt := in.StartsAt.Add(-5 * time.Minute)
	if remindAt.After(time.Now()) {
		_, _ = s.db.Exec(r.Context(), `INSERT INTO focusflow.reminders(user_id,calendar_block_id,title,remind_at) VALUES($1,$2,$3,$4)`, u.ID, id, in.Title, remindAt)
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "version": 1})
}

func (s *Server) patchCalendarBlock(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, 400, "invalid_id", "Некорректный блок", nil)
		return
	}
	var in struct {
		StartsAt time.Time `json:"startsAt"`
		EndsAt   time.Time `json:"endsAt"`
		Version  int       `json:"version"`
		Locked   *bool     `json:"locked"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if !in.EndsAt.After(in.StartsAt) {
		writeError(w, r, 422, "validation_error", "Некорректный интервал", nil)
		return
	}
	var version int
	err = s.db.QueryRow(r.Context(), `UPDATE focusflow.calendar_blocks SET starts_at=$3,ends_at=$4,locked=coalesce($5,locked),updated_at=now(),version=version+1 WHERE id=$1 AND user_id=$2 AND version=$6 RETURNING version`, id, u.ID, in.StartsAt, in.EndsAt, in.Locked, in.Version).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, 409, "version_conflict", "Блок уже изменён", nil)
		return
	}
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "version": version})
}

func (s *Server) deleteCalendarBlock(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, 400, "invalid_id", "Некорректный блок", nil)
		return
	}
	_, err = s.db.Exec(r.Context(), `DELETE FROM focusflow.calendar_blocks WHERE id=$1 AND user_id=$2`, id, u.ID)
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	w.WriteHeader(204)
}

func (s *Server) getTimer(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	timer, err := s.loadTimer(r.Context(), u.ID)
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	writeJSON(w, 200, timer)
}

func (s *Server) loadTimer(ctx context.Context, userID uuid.UUID) (map[string]any, error) {
	var taskID, goalID, projectID, blockID *uuid.UUID
	var mode, status string
	var started *time.Time
	var acc int
	var target *int
	var updated time.Time
	var version int
	var taskTitle *string
	err := s.db.QueryRow(ctx, `SELECT ts.task_id,ts.goal_id,ts.project_id,ts.calendar_block_id,ts.mode,ts.status,ts.started_at,ts.accumulated_seconds,ts.target_seconds,ts.updated_at,ts.version,t.title FROM focusflow.timer_state ts LEFT JOIN focusflow.tasks t ON t.id=ts.task_id WHERE ts.user_id=$1`, userID).Scan(&taskID, &goalID, &projectID, &blockID, &mode, &status, &started, &acc, &target, &updated, &version, &taskTitle)
	if errors.Is(err, pgx.ErrNoRows) {
		return map[string]any{"status": "idle", "mode": "stopwatch", "accumulatedSeconds": 0, "version": 0}, nil
	}
	if err != nil {
		return nil, err
	}
	elapsed := acc
	if status == "running" && started != nil {
		elapsed += int(time.Since(*started).Seconds())
	}
	return map[string]any{"taskId": taskID, "goalId": goalID, "projectId": projectID, "calendarBlockId": blockID, "mode": mode, "status": status, "startedAt": started, "accumulatedSeconds": acc, "elapsedSeconds": max(elapsed, 0), "targetSeconds": target, "updatedAt": updated, "version": version, "taskTitle": taskTitle}, nil
}

func (s *Server) startTimer(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var in struct {
		TaskID          *uuid.UUID `json:"taskId"`
		GoalID          *uuid.UUID `json:"goalId"`
		ProjectID       *uuid.UUID `json:"projectId"`
		CalendarBlockID *uuid.UUID `json:"calendarBlockId"`
		Mode            string     `json:"mode"`
		TargetSeconds   *int       `json:"targetSeconds"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Mode == "" {
		in.Mode = "stopwatch"
	}
	var existing string
	_ = s.db.QueryRow(r.Context(), `SELECT status FROM focusflow.timer_state WHERE user_id=$1`, u.ID).Scan(&existing)
	if existing == "running" || existing == "paused" {
		writeError(w, r, 409, "timer_active", "Сначала завершите текущий таймер", nil)
		return
	}
	_, err := s.db.Exec(r.Context(), `INSERT INTO focusflow.timer_state(user_id,task_id,goal_id,project_id,calendar_block_id,mode,status,started_at,accumulated_seconds,target_seconds) VALUES($1,$2,$3,$4,$5,$6,'running',now(),0,$7) ON CONFLICT(user_id) DO UPDATE SET task_id=excluded.task_id,goal_id=excluded.goal_id,project_id=excluded.project_id,calendar_block_id=excluded.calendar_block_id,mode=excluded.mode,status='running',started_at=now(),accumulated_seconds=0,target_seconds=excluded.target_seconds,updated_at=now(),version=focusflow.timer_state.version+1`, u.ID, in.TaskID, in.GoalID, in.ProjectID, in.CalendarBlockID, in.Mode, in.TargetSeconds)
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	timer, _ := s.loadTimer(r.Context(), u.ID)
	writeJSON(w, 200, timer)
}

func (s *Server) pauseTimer(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	tag, err := s.db.Exec(r.Context(), `UPDATE focusflow.timer_state SET accumulated_seconds=accumulated_seconds+greatest(extract(epoch from now()-started_at)::integer,0),status='paused',started_at=NULL,updated_at=now(),version=version+1 WHERE user_id=$1 AND status='running'`, u.ID)
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, r, 409, "timer_not_running", "Таймер не запущен", nil)
		return
	}
	timer, _ := s.loadTimer(r.Context(), u.ID)
	writeJSON(w, 200, timer)
}
func (s *Server) resumeTimer(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	tag, err := s.db.Exec(r.Context(), `UPDATE focusflow.timer_state SET status='running',started_at=now(),updated_at=now(),version=version+1 WHERE user_id=$1 AND status='paused'`, u.ID)
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, r, 409, "timer_not_paused", "Таймер не на паузе", nil)
		return
	}
	timer, _ := s.loadTimer(r.Context(), u.ID)
	writeJSON(w, 200, timer)
}

func (s *Server) stopTimer(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	var taskID, goalID, projectID, blockID *uuid.UUID
	var status string
	var started *time.Time
	var acc int
	err = tx.QueryRow(r.Context(), `SELECT task_id,goal_id,project_id,calendar_block_id,status,started_at,accumulated_seconds FROM focusflow.timer_state WHERE user_id=$1 FOR UPDATE`, u.ID).Scan(&taskID, &goalID, &projectID, &blockID, &status, &started, &acc)
	if errors.Is(err, pgx.ErrNoRows) || status == "idle" {
		writeError(w, r, 409, "timer_idle", "Таймер уже остановлен", nil)
		return
	}
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	total := acc
	end := time.Now()
	if status == "running" && started != nil {
		total += max(int(end.Sub(*started).Seconds()), 0)
	}
	entryStart := end.Add(-time.Duration(total) * time.Second)
	var entryID uuid.UUID
	if total > 0 {
		err = tx.QueryRow(r.Context(), `INSERT INTO focusflow.time_entries(user_id,task_id,goal_id,project_id,calendar_block_id,started_at,ended_at,duration_seconds) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`, u.ID, taskID, goalID, projectID, blockID, entryStart, end, total).Scan(&entryID)
		if err != nil {
			writeDBError(w, r, err)
			return
		}
	}
	_, err = tx.Exec(r.Context(), `UPDATE focusflow.timer_state SET status='idle',started_at=NULL,accumulated_seconds=0,task_id=NULL,goal_id=NULL,project_id=NULL,calendar_block_id=NULL,updated_at=now(),version=version+1 WHERE user_id=$1`, u.ID)
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeDBError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"entryId": entryID, "durationSeconds": total, "status": "idle"})
}

func (s *Server) listNotes(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	rows, err := s.db.Query(r.Context(), `SELECT id,parent_id,title,content,plain_text,icon,position,updated_at,version FROM focusflow.notes WHERE user_id=$1 AND archived_at IS NULL ORDER BY parent_id NULLS FIRST,position,title`, u.ID)
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id uuid.UUID
		var parent *uuid.UUID
		var title, plain, icon string
		var content json.RawMessage
		var pos, version int
		var updated time.Time
		if err := rows.Scan(&id, &parent, &title, &content, &plain, &icon, &pos, &updated, &version); err != nil {
			writeDBError(w, r, err)
			return
		}
		items = append(items, map[string]any{"id": id, "parentId": parent, "title": title, "content": content, "plainText": plain, "icon": icon, "position": pos, "updatedAt": updated, "version": version})
	}
	writeJSON(w, 200, items)
}

func (s *Server) createNote(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var in struct {
		Title    string     `json:"title"`
		ParentID *uuid.UUID `json:"parentId"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Title) == "" {
		in.Title = "Без названия"
	}
	var id uuid.UUID
	err := s.db.QueryRow(r.Context(), `INSERT INTO focusflow.notes(user_id,parent_id,title) VALUES($1,$2,$3) RETURNING id`, u.ID, in.ParentID, in.Title).Scan(&id)
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	writeJSON(w, 201, map[string]any{"id": id, "title": in.Title, "content": []any{}, "version": 1})
}
func (s *Server) patchNote(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, 400, "invalid_id", "Некорректная заметка", nil)
		return
	}
	var in struct {
		Title     *string         `json:"title"`
		Content   json.RawMessage `json:"content"`
		PlainText *string         `json:"plainText"`
		Version   int             `json:"version"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	hasContent := len(in.Content) > 0
	var version int
	err = s.db.QueryRow(r.Context(), `UPDATE focusflow.notes SET title=coalesce($3,title),content=CASE WHEN $4 THEN $5::jsonb ELSE content END,plain_text=coalesce($6,plain_text),updated_at=now(),version=version+1 WHERE id=$1 AND user_id=$2 AND version=$7 RETURNING version`, id, u.ID, in.Title, hasContent, in.Content, in.PlainText, in.Version).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, 409, "version_conflict", "Заметка изменена на другом устройстве", nil)
		return
	}
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"id": id, "version": version})
}
func (s *Server) archiveNote(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, 400, "invalid_id", "Некорректная заметка", nil)
		return
	}
	_, err = s.db.Exec(r.Context(), `UPDATE focusflow.notes SET archived_at=now(),updated_at=now(),version=version+1 WHERE id=$1 AND user_id=$2`, id, u.ID)
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	w.WriteHeader(204)
}

func (s *Server) uploadAttachment(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	noteID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_id", "Некорректная заметка", nil)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 11<<20)
	if err := r.ParseMultipartForm(11 << 20); err != nil {
		writeError(w, r, http.StatusRequestEntityTooLarge, "file_too_large", "Максимальный размер вложения — 10 МБ", nil)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "file_required", "Выберите файл", nil)
		return
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, (10<<20)+1))
	if err != nil || len(content) > 10<<20 {
		writeError(w, r, http.StatusRequestEntityTooLarge, "file_too_large", "Максимальный размер вложения — 10 МБ", nil)
		return
	}
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = http.DetectContentType(content)
	}
	var id uuid.UUID
	err = s.db.QueryRow(r.Context(), `
		INSERT INTO focusflow.attachments(user_id,note_id,file_name,content_type,size_bytes,content)
		SELECT $1,n.id,$3,$4,$5,$6 FROM focusflow.notes n WHERE n.id=$2 AND n.user_id=$1 AND n.archived_at IS NULL
		RETURNING id`, u.ID, noteID, filepath.Base(header.Filename), contentType, len(content), content).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "not_found", "Заметка не найдена", nil)
		return
	}
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "url": "/api/v1/attachments/" + id.String(), "name": header.Filename, "size": len(content)})
}

func (s *Server) getAttachment(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_id", "Некорректное вложение", nil)
		return
	}
	var name, contentType string
	var content []byte
	err = s.db.QueryRow(r.Context(), `SELECT file_name,content_type,content FROM focusflow.attachments WHERE id=$1 AND user_id=$2`, id, u.ID).Scan(&name, &contentType, &content)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, http.StatusNotFound, "not_found", "Вложение не найдено", nil)
		return
	}
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, strings.ReplaceAll(name, `"`, "")))
	w.Header().Set("Cache-Control", "private, max-age=3600")
	_, _ = w.Write(content)
}

func (s *Server) listDecks(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	rows, err := s.db.Query(r.Context(), `SELECT d.id,d.name,d.color,d.new_per_day,d.reviews_per_day,d.desired_retention,d.version,count(c.id),count(c.id) FILTER(WHERE c.due_at<=now() AND c.suspended_at IS NULL AND c.archived_at IS NULL) FROM focusflow.decks d LEFT JOIN focusflow.cards c ON c.deck_id=d.id WHERE d.user_id=$1 AND d.archived_at IS NULL GROUP BY d.id ORDER BY d.name`, u.ID)
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id uuid.UUID
		var name, color string
		var newDay, reviews, version, total, due int
		var retention float64
		if err := rows.Scan(&id, &name, &color, &newDay, &reviews, &retention, &version, &total, &due); err != nil {
			writeDBError(w, r, err)
			return
		}
		items = append(items, map[string]any{"id": id, "name": name, "color": color, "newPerDay": newDay, "reviewsPerDay": reviews, "desiredRetention": retention, "version": version, "total": total, "due": due})
	}
	writeJSON(w, 200, items)
}
func (s *Server) createDeck(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var in struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Name == "" {
		writeError(w, r, 422, "validation_error", "Введите название колоды", nil)
		return
	}
	if in.Color == "" {
		in.Color = "#2f9d74"
	}
	var id uuid.UUID
	err := s.db.QueryRow(r.Context(), `INSERT INTO focusflow.decks(user_id,name,color) VALUES($1,$2,$3) RETURNING id`, u.ID, in.Name, in.Color).Scan(&id)
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	writeJSON(w, 201, map[string]any{"id": id, "version": 1})
}

func (s *Server) listCards(w http.ResponseWriter, r *http.Request) { s.cardsQuery(w, r, false) }
func (s *Server) dueCards(w http.ResponseWriter, r *http.Request)  { s.cardsQuery(w, r, true) }
func (s *Server) cardsQuery(w http.ResponseWriter, r *http.Request, dueOnly bool) {
	u := currentUser(r)
	query := `SELECT c.id,c.deck_id,d.name,c.card_type,c.front,c.back,c.tags,c.due_at,c.fsrs_state,c.source_note_id,c.version FROM focusflow.cards c JOIN focusflow.decks d ON d.id=c.deck_id WHERE c.user_id=$1 AND c.archived_at IS NULL AND c.suspended_at IS NULL`
	if dueOnly {
		query += ` AND c.due_at<=now()`
	}
	query += ` ORDER BY c.due_at LIMIT 200`
	rows, err := s.db.Query(r.Context(), query, u.ID)
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	scheduler := fsrs.NewFSRS(fsrs.DefaultParam())
	for rows.Next() {
		var id, deckID uuid.UUID
		var deckName, typ, front, back string
		var tags []string
		var due time.Time
		var state json.RawMessage
		var noteID *uuid.UUID
		var version int
		if err := rows.Scan(&id, &deckID, &deckName, &typ, &front, &back, &tags, &due, &state, &noteID, &version); err != nil {
			writeDBError(w, r, err)
			return
		}
		card := fsrs.NewCard(time.Now())
		if len(state) > 2 {
			_ = json.Unmarshal(state, &card)
		}
		previewMap := map[string]time.Time{}
		if p, e := scheduler.Repeat(card, time.Now()); e == nil {
			previewMap["again"] = p[fsrs.Again].Card.Due
			previewMap["hard"] = p[fsrs.Hard].Card.Due
			previewMap["good"] = p[fsrs.Good].Card.Due
			previewMap["easy"] = p[fsrs.Easy].Card.Due
		}
		items = append(items, map[string]any{"id": id, "deckId": deckID, "deckName": deckName, "cardType": typ, "front": front, "back": back, "tags": tags, "dueAt": due, "sourceNoteId": noteID, "version": version, "preview": previewMap})
	}
	writeJSON(w, 200, items)
}

func (s *Server) createCard(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var in struct {
		DeckID       uuid.UUID  `json:"deckId"`
		Front        string     `json:"front"`
		Back         string     `json:"back"`
		CardType     string     `json:"cardType"`
		Tags         []string   `json:"tags"`
		SourceNoteID *uuid.UUID `json:"sourceNoteId"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Front == "" || in.DeckID == uuid.Nil {
		writeError(w, r, 422, "validation_error", "Заполните вопрос и выберите колоду", nil)
		return
	}
	if in.CardType == "" {
		in.CardType = "basic"
	}
	card := fsrs.NewCard(time.Now())
	state, _ := json.Marshal(card)
	var id uuid.UUID
	err := s.db.QueryRow(r.Context(), `INSERT INTO focusflow.cards(user_id,deck_id,source_note_id,card_type,front,back,tags,fsrs_state,due_at) SELECT $1,d.id,$3,$4,$5,$6,$7,$8,$9 FROM focusflow.decks d WHERE d.id=$2 AND d.user_id=$1 RETURNING id`, u.ID, in.DeckID, in.SourceNoteID, in.CardType, in.Front, in.Back, in.Tags, state, time.Now()).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, 404, "deck_not_found", "Колода не найдена", nil)
		return
	}
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	writeJSON(w, 201, map[string]any{"id": id, "version": 1})
}

func (s *Server) reviewCard(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, 400, "invalid_id", "Некорректная карточка", nil)
		return
	}
	var in struct {
		Rating     int `json:"rating"`
		DurationMS int `json:"durationMs"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Rating < 1 || in.Rating > 4 {
		writeError(w, r, 422, "validation_error", "Оценка должна быть от 1 до 4", nil)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	var raw json.RawMessage
	var due time.Time
	err = tx.QueryRow(r.Context(), `SELECT fsrs_state,due_at FROM focusflow.cards WHERE id=$1 AND user_id=$2 AND archived_at IS NULL FOR UPDATE`, id, u.ID).Scan(&raw, &due)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, r, 404, "not_found", "Карточка не найдена", nil)
		return
	}
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	now := time.Now()
	card := fsrs.NewCard(now)
	if len(raw) > 2 {
		_ = json.Unmarshal(raw, &card)
	}
	scheduler := fsrs.NewFSRS(fsrs.DefaultParam())
	grade := fsrs.Rating(in.Rating)
	next, err := scheduler.Next(card, now, grade)
	if err != nil {
		writeError(w, r, 422, "fsrs_error", "Не удалось рассчитать повторение", nil)
		return
	}
	nextRaw, _ := json.Marshal(next.Card)
	scheduledDays := int(math.Round(next.Card.Due.Sub(now).Hours() / 24))
	_, err = tx.Exec(r.Context(), `UPDATE focusflow.cards SET fsrs_state=$3,due_at=$4,updated_at=now(),version=version+1 WHERE id=$1 AND user_id=$2`, id, u.ID, nextRaw, next.Card.Due)
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO focusflow.review_logs(user_id,card_id,rating,reviewed_at,duration_ms,previous_state,next_state,scheduled_days) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, u.ID, id, in.Rating, now, in.DurationMS, raw, nextRaw, scheduledDays)
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeDBError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"cardId": id, "dueAt": next.Card.Due, "scheduledDays": scheduledDays, "versionDelta": 1})
}

func (s *Server) ankiExport(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	rows, err := s.db.Query(r.Context(), `SELECT c.id,c.front,c.back,c.card_type,c.tags,d.name,al.anki_note_id,al.last_local_hash,al.last_anki_hash FROM focusflow.cards c JOIN focusflow.decks d ON d.id=c.deck_id LEFT JOIN focusflow.anki_links al ON al.card_id=c.id WHERE c.user_id=$1 AND c.archived_at IS NULL ORDER BY c.updated_at`, u.ID)
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id uuid.UUID
		var front, back, typ, deck string
		var tags []string
		var noteID *int64
		var localHash, ankiHash *string
		if err := rows.Scan(&id, &front, &back, &typ, &tags, &deck, &noteID, &localHash, &ankiHash); err != nil {
			writeDBError(w, r, err)
			return
		}
		items = append(items, map[string]any{"id": id, "front": front, "back": back, "cardType": typ, "tags": tags, "deck": deck, "ankiNoteId": noteID, "lastLocalHash": localHash, "lastAnkiHash": ankiHash})
	}
	writeJSON(w, 200, items)
}

func (s *Server) ankiImport(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var in struct {
		DeckID uuid.UUID `json:"deckId"`
		Notes  []struct {
			AnkiNoteID int64    `json:"ankiNoteId"`
			Deck       string   `json:"deck"`
			Model      string   `json:"model"`
			Front      string   `json:"front"`
			Back       string   `json:"back"`
			Tags       []string `json:"tags"`
			Hash       string   `json:"hash"`
		} `json:"notes"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	imported, updated, conflicts := 0, 0, 0
	for _, n := range in.Notes {
		var linkID, cardID uuid.UUID
		var localHash, ankiHash *string
		err = tx.QueryRow(r.Context(), `SELECT al.id,al.card_id,al.last_local_hash,al.last_anki_hash FROM focusflow.anki_links al WHERE al.user_id=$1 AND al.anki_note_id=$2`, u.ID, n.AnkiNoteID).Scan(&linkID, &cardID, &localHash, &ankiHash)
		if errors.Is(err, pgx.ErrNoRows) {
			state, _ := json.Marshal(fsrs.NewCard(time.Now()))
			err = tx.QueryRow(r.Context(), `INSERT INTO focusflow.cards(user_id,deck_id,front,back,tags,fsrs_state) VALUES($1,$2,$3,$4,$5,$6) RETURNING id`, u.ID, in.DeckID, n.Front, n.Back, n.Tags, state).Scan(&cardID)
			if err != nil {
				writeDBError(w, r, err)
				return
			}
			_, err = tx.Exec(r.Context(), `INSERT INTO focusflow.anki_links(user_id,card_id,anki_note_id,anki_deck,anki_model,last_local_hash,last_anki_hash,last_synced_at) VALUES($1,$2,$3,$4,$5,$6,$6,now())`, u.ID, cardID, n.AnkiNoteID, n.Deck, n.Model, n.Hash)
			if err != nil {
				writeDBError(w, r, err)
				return
			}
			imported++
			continue
		}
		if err != nil {
			writeDBError(w, r, err)
			return
		}
		if localHash != nil && ankiHash != nil && *localHash != *ankiHash && *ankiHash != n.Hash {
			_, _ = tx.Exec(r.Context(), `UPDATE focusflow.anki_links SET conflict=$3 WHERE id=$1 AND user_id=$2`, linkID, u.ID, map[string]any{"remoteFront": n.Front, "remoteBack": n.Back, "remoteHash": n.Hash})
			conflicts++
			continue
		}
		_, err = tx.Exec(r.Context(), `UPDATE focusflow.cards SET front=$3,back=$4,tags=$5,updated_at=now(),version=version+1 WHERE id=$1 AND user_id=$2`, cardID, u.ID, n.Front, n.Back, n.Tags)
		if err != nil {
			writeDBError(w, r, err)
			return
		}
		_, _ = tx.Exec(r.Context(), `UPDATE focusflow.anki_links SET last_local_hash=$3,last_anki_hash=$3,last_synced_at=now(),conflict=NULL WHERE id=$1 AND user_id=$2`, linkID, u.ID, n.Hash)
		updated++
	}
	runID := uuid.New()
	_, _ = tx.Exec(r.Context(), `INSERT INTO focusflow.anki_sync_runs(id,user_id,status,imported,conflicts,finished_at) VALUES($1,$2,'completed',$3,$4,now())`, runID, u.ID, imported, conflicts)
	if err := tx.Commit(r.Context()); err != nil {
		writeDBError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"runId": runID, "imported": imported, "updated": updated, "conflicts": conflicts})
}

func (s *Server) ankiStatus(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var linked, conflicts int
	var lastSync *time.Time
	_ = s.db.QueryRow(r.Context(), `SELECT count(*),count(*) FILTER(WHERE conflict IS NOT NULL),max(last_synced_at) FROM focusflow.anki_links WHERE user_id=$1 AND archived_at IS NULL`, u.ID).Scan(&linked, &conflicts, &lastSync)
	writeJSON(w, 200, map[string]any{"linked": linked, "conflicts": conflicts, "lastSyncAt": lastSync})
}

func (s *Server) ankiLink(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var in struct {
		CardID     uuid.UUID `json:"cardId"`
		AnkiNoteID int64     `json:"ankiNoteId"`
		Deck       string    `json:"deck"`
		Model      string    `json:"model"`
		Hash       string    `json:"hash"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.CardID == uuid.Nil || in.AnkiNoteID == 0 || in.Deck == "" {
		writeError(w, r, 422, "validation_error", "Некорректная связь Anki", nil)
		return
	}
	tag, err := s.db.Exec(r.Context(), `
		INSERT INTO focusflow.anki_links(user_id,card_id,anki_note_id,anki_deck,anki_model,last_local_hash,last_anki_hash,last_synced_at)
		SELECT $1,c.id,$3,$4,$5,$6,$6,now() FROM focusflow.cards c WHERE c.id=$2 AND c.user_id=$1
		ON CONFLICT(card_id) DO UPDATE SET anki_note_id=excluded.anki_note_id,anki_deck=excluded.anki_deck,anki_model=excluded.anki_model,last_local_hash=excluded.last_local_hash,last_anki_hash=excluded.last_anki_hash,last_synced_at=now(),conflict=NULL`,
		u.ID, in.CardID, in.AnkiNoteID, in.Deck, in.Model, in.Hash)
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, r, 404, "not_found", "Карточка не найдена", nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listNotifications(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	rows, err := s.db.Query(r.Context(), `SELECT id,title,remind_at,status,task_id,calendar_block_id,version FROM focusflow.reminders WHERE user_id=$1 AND status!='cancelled' ORDER BY remind_at DESC LIMIT 100`, u.ID)
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id uuid.UUID
		var title, status string
		var at time.Time
		var taskID, blockID *uuid.UUID
		var version int
		if err := rows.Scan(&id, &title, &at, &status, &taskID, &blockID, &version); err != nil {
			writeDBError(w, r, err)
			return
		}
		items = append(items, map[string]any{"id": id, "title": title, "remindAt": at, "status": status, "taskId": taskID, "calendarBlockId": blockID, "version": version})
	}
	writeJSON(w, 200, items)
}
func (s *Server) snoozeNotification(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, 400, "invalid_id", "Некорректное напоминание", nil)
		return
	}
	var in struct {
		Minutes int `json:"minutes"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Minutes <= 0 {
		in.Minutes = 10
	}
	tag, err := s.db.Exec(r.Context(), `UPDATE focusflow.reminders SET remind_at=now()+($3::text||' minutes')::interval,status='snoozed',updated_at=now(),version=version+1 WHERE id=$1 AND user_id=$2`, id, u.ID, in.Minutes)
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, r, 404, "not_found", "Напоминание не найдено", nil)
		return
	}
	writeJSON(w, 200, map[string]any{"id": id, "snoozedMinutes": in.Minutes})
}

func (s *Server) savePushSubscription(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var in struct {
		Endpoint string `json:"endpoint"`
		Keys     struct {
			P256dh string `json:"p256dh"`
			Auth   string `json:"auth"`
		} `json:"keys"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Endpoint == "" || in.Keys.P256dh == "" || in.Keys.Auth == "" {
		writeError(w, r, 422, "validation_error", "Некорректная push-подписка", nil)
		return
	}
	_, err := s.db.Exec(r.Context(), `INSERT INTO focusflow.push_subscriptions(user_id,endpoint,p256dh,auth,user_agent) VALUES($1,$2,$3,$4,$5) ON CONFLICT(endpoint) DO UPDATE SET user_id=excluded.user_id,p256dh=excluded.p256dh,auth=excluded.auth,user_agent=excluded.user_agent,last_used_at=now()`, u.ID, in.Endpoint, in.Keys.P256dh, in.Keys.Auth, r.UserAgent())
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	w.WriteHeader(204)
}

func (s *Server) analytics(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	rows, err := s.db.Query(r.Context(), `SELECT (started_at AT TIME ZONE $2)::date,coalesce(sum(duration_seconds),0) FROM focusflow.time_entries WHERE user_id=$1 AND started_at>=now()-interval '13 days' GROUP BY 1 ORDER BY 1`, u.ID, u.Timezone)
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	defer rows.Close()
	series := make([]map[string]any, 0)
	var total int64
	for rows.Next() {
		var day time.Time
		var seconds int64
		if err := rows.Scan(&day, &seconds); err != nil {
			writeDBError(w, r, err)
			return
		}
		total += seconds
		series = append(series, map[string]any{"date": day.Format("2006-01-02"), "seconds": seconds})
	}
	var reviews, correct int
	_ = s.db.QueryRow(r.Context(), `SELECT count(*),count(*) FILTER(WHERE rating>1) FROM focusflow.review_logs WHERE user_id=$1 AND reviewed_at>=now()-interval '30 days'`, u.ID).Scan(&reviews, &correct)
	retention := 0.0
	if reviews > 0 {
		retention = math.Round(float64(correct)/float64(reviews)*1000) / 10
	}
	writeJSON(w, 200, map[string]any{"focusSeries": series, "focusTotalSeconds": total, "reviews30d": reviews, "retention": retention})
}

func (s *Server) exportICS(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	rows, err := s.db.Query(r.Context(), `SELECT id,title,starts_at,ends_at FROM focusflow.calendar_blocks WHERE user_id=$1 ORDER BY starts_at`, u.ID)
	if err != nil {
		writeDBError(w, r, err)
		return
	}
	defer rows.Close()
	var b strings.Builder
	b.WriteString("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//FocusFlow//RU\r\nCALSCALE:GREGORIAN\r\n")
	for rows.Next() {
		var id uuid.UUID
		var title string
		var start, end time.Time
		if err := rows.Scan(&id, &title, &start, &end); err != nil {
			writeDBError(w, r, err)
			return
		}
		b.WriteString("BEGIN:VEVENT\r\nUID:" + id.String() + "@focusflow\r\nDTSTAMP:" + icsTime(time.Now()) + "\r\nDTSTART:" + icsTime(start) + "\r\nDTEND:" + icsTime(end) + "\r\nSUMMARY:" + escapeICS(title) + "\r\nEND:VEVENT\r\n")
	}
	b.WriteString("END:VCALENDAR\r\n")
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=focusflow.ics")
	_, _ = io.WriteString(w, b.String())
}

func (s *Server) importICS(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2<<20))
	if err != nil {
		writeError(w, r, 413, "file_too_large", "ICS-файл слишком большой", nil)
		return
	}
	events := parseICS(string(body))
	created := 0
	for _, e := range events {
		if e.Title == "" || !e.End.After(e.Start) {
			continue
		}
		_, err = s.db.Exec(r.Context(), `INSERT INTO focusflow.calendar_blocks(user_id,title,starts_at,ends_at,source,external_uid,color) VALUES($1,$2,$3,$4,'ics',$5,'#8b8b97') ON CONFLICT DO NOTHING`, u.ID, e.Title, e.Start, e.End, e.UID)
		if err == nil {
			created++
		}
	}
	writeJSON(w, 201, map[string]any{"created": created})
}

func (s *Server) StartWorker(ctx context.Context) {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.processReminders(ctx)
		}
	}
}

func (s *Server) processReminders(ctx context.Context) {
	rows, err := s.db.Query(ctx, `WITH due AS (SELECT id,user_id,title FROM focusflow.reminders WHERE status IN ('scheduled','snoozed') AND remind_at<=now() ORDER BY remind_at FOR UPDATE SKIP LOCKED LIMIT 50) UPDATE focusflow.reminders r SET status='sent',updated_at=now(),version=version+1 FROM due WHERE r.id=due.id RETURNING due.id,due.user_id,due.title`)
	if err != nil {
		s.log.Error("reminder_worker", "error", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id, userID uuid.UUID
		var title string
		if rows.Scan(&id, &userID, &title) != nil {
			continue
		}
		key := "in_app:" + id.String()
		_, _ = s.db.Exec(ctx, `INSERT INTO focusflow.reminder_deliveries(reminder_id,channel,status,idempotency_key,delivered_at) VALUES($1,'in_app','delivered',$2,now()) ON CONFLICT(idempotency_key) DO NOTHING`, id, key)
		_, _ = s.db.Exec(ctx, `INSERT INTO focusflow.background_jobs(kind,payload,idempotency_key) VALUES('push.reminder',jsonb_build_object('reminderId',$1,'userId',$2,'title',$3),$4) ON CONFLICT(idempotency_key) DO NOTHING`, id, userID, title, "push:"+id.String())
		s.deliverPush(ctx, id, userID, title)
	}
}

func (s *Server) deliverPush(ctx context.Context, reminderID, userID uuid.UUID, title string) {
	if s.cfg.VAPIDPublicKey == "" || s.cfg.VAPIDPrivateKey == "" {
		return
	}
	rows, err := s.db.Query(ctx, `SELECT id,endpoint,p256dh,auth FROM focusflow.push_subscriptions WHERE user_id=$1`, userID)
	if err != nil {
		s.log.Error("push_subscriptions", "error", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var subscriptionID uuid.UUID
		var endpoint, p256dh, authKey string
		if err := rows.Scan(&subscriptionID, &endpoint, &p256dh, &authKey); err != nil {
			continue
		}
		key := "push:" + reminderID.String() + ":" + subscriptionID.String()
		var exists bool
		_ = s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM focusflow.reminder_deliveries WHERE idempotency_key=$1)`, key).Scan(&exists)
		if exists {
			continue
		}
		payload, _ := json.Marshal(map[string]any{"title": "FocusFlow", "body": title, "url": "/?notification=" + reminderID.String()})
		response, sendErr := webpush.SendNotification(payload, &webpush.Subscription{
			Endpoint: endpoint,
			Keys:     webpush.Keys{P256dh: p256dh, Auth: authKey},
		}, &webpush.Options{
			Subscriber: s.cfg.VAPIDSubject, VAPIDPublicKey: s.cfg.VAPIDPublicKey,
			VAPIDPrivateKey: s.cfg.VAPIDPrivateKey, TTL: 300,
		})
		status := "delivered"
		var errorText *string
		if sendErr != nil {
			status = "failed"
			message := sendErr.Error()
			errorText = &message
		} else if response != nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusGone || response.StatusCode == http.StatusNotFound {
				_, _ = s.db.Exec(ctx, `DELETE FROM focusflow.push_subscriptions WHERE id=$1`, subscriptionID)
			}
			if response.StatusCode < 200 || response.StatusCode >= 300 {
				status = "failed"
				message := response.Status
				errorText = &message
			}
		}
		_, _ = s.db.Exec(ctx, `INSERT INTO focusflow.reminder_deliveries(reminder_id,channel,status,idempotency_key,error,delivered_at) VALUES($1,'push',$2,$3,$4,CASE WHEN $2='delivered' THEN now() END) ON CONFLICT(idempotency_key) DO NOTHING`, reminderID, status, key, errorText)
	}
}

func seedWorkspace(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	var projectID, goalID, deckID, noteID uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO focusflow.projects(user_id,name,color,icon) VALUES($1,'Развитие','#5b5bd6','sparkles') RETURNING id`, userID).Scan(&projectID); err != nil {
		return err
	}
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, -1)
	if err := tx.QueryRow(ctx, `INSERT INTO focusflow.goals(user_id,project_id,title,target_seconds,period_start,period_end,max_daily_seconds,session_seconds,color) VALUES($1,$2,'Глубокая работа',120*3600,$3,$4,8*3600,50*60,'#5b5bd6') RETURNING id`, userID, projectID, start, end).Scan(&goalID); err != nil {
		return err
	}
	tasks := []struct {
		title    string
		priority int
		due      time.Time
		estimate int
	}{{"Подготовить план недели", 1, now.Add(2 * time.Hour), 30 * 60}, {"Разобрать заметки по Go", 2, now.Add(24 * time.Hour), 50 * 60}, {"Настроить AnkiConnect", 3, now.Add(48 * time.Hour), 25 * 60}, {"Провести недельный обзор", 2, now.AddDate(0, 0, 5), 45 * 60}}
	for _, t := range tasks {
		_, err := tx.Exec(ctx, `INSERT INTO focusflow.tasks(user_id,project_id,goal_id,title,priority,due_at,estimate_seconds) VALUES($1,$2,$3,$4,$5,$6,$7)`, userID, projectID, goalID, t.title, t.priority, t.due, t.estimate)
		if err != nil {
			return err
		}
	}
	for i, title := range []string{"Планирование цели", "Сфокусированная работа", "Повторение карточек"} {
		startAt := time.Date(now.Year(), now.Month(), now.Day()+i, 10+i, 0, 0, 0, time.Local)
		_, err := tx.Exec(ctx, `INSERT INTO focusflow.calendar_blocks(user_id,goal_id,title,starts_at,ends_at,color,source,locked) VALUES($1,$2,$3,$4,$5,'#5b5bd6','manual',true)`, userID, goalID, title, startAt, startAt.Add(50*time.Minute))
		if err != nil {
			return err
		}
	}
	content := json.RawMessage(`[{"id":"intro","type":"heading","props":{"level":1},"content":[{"type":"text","text":"Система продуктивности","styles":{}}],"children":[]},{"id":"body","type":"paragraph","props":{},"content":[{"type":"text","text":"Свяжите задачи, календарь, время и знания в одном рабочем пространстве.","styles":{}}],"children":[]}]`)
	if err := tx.QueryRow(ctx, `INSERT INTO focusflow.notes(user_id,title,content,plain_text,icon) VALUES($1,'Добро пожаловать в FocusFlow',$2,'Система продуктивности. Свяжите задачи, календарь, время и знания в одном рабочем пространстве.','sparkles') RETURNING id`, userID, content).Scan(&noteID); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, `INSERT INTO focusflow.decks(user_id,name,color) VALUES($1,'FocusFlow — основы','#2f9d74') RETURNING id`, userID).Scan(&deckID); err != nil {
		return err
	}
	cards := []struct{ front, back string }{{"Что делает месячный планировщик?", "Распределяет оставшееся целевое время по доступным дням и календарным окнам."}, {"Что означает оценка Again?", "Карточка забыта и должна вернуться после короткого интервала."}, {"Когда AnkiConnect доступен?", "Когда Anki Desktop запущен на том же компьютере."}}
	for _, c := range cards {
		state, _ := json.Marshal(fsrs.NewCard(now))
		_, err := tx.Exec(ctx, `INSERT INTO focusflow.cards(user_id,deck_id,source_note_id,front,back,fsrs_state,due_at) VALUES($1,$2,$3,$4,$5,$6,now())`, userID, deckID, noteID, c.front, c.back, state)
		if err != nil {
			return err
		}
	}
	_, err := tx.Exec(ctx, `INSERT INTO focusflow.timer_state(user_id) VALUES($1)`, userID)
	return err
}

func goalDays(timezone string, g goalResponse, now time.Time) []planner.Day {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
	}
	today := now.In(loc)
	start := g.PeriodStart.In(loc)
	if today.After(start) {
		start = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, loc)
	}
	end := g.PeriodEnd.In(loc)
	selected := map[int]bool{}
	for _, d := range g.Workdays {
		selected[int(d)] = true
	}
	sh, sm := parseClock(g.PreferredStart)
	eh, em := parseClock(g.PreferredEnd)
	window := int64((time.Duration(eh*60+em-sh*60-sm) * time.Minute).Seconds())
	capacity := min64(int64(g.MaxDailySeconds), window)
	var days []planner.Day
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		if selected[int(d.Weekday())] {
			days = append(days, planner.Day{Date: d, CapacitySeconds: capacity})
		}
	}
	return days
}

type calendarQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type timeSpan struct {
	Start time.Time
	End   time.Time
}

func calendarCapacity(ctx context.Context, query calendarQuerier, userID uuid.UUID, g goalResponse, days []planner.Day, timezone string) ([]planner.Day, map[string][]timeSpan, error) {
	occupied := make(map[string][]timeSpan)
	if len(days) == 0 {
		return days, occupied, nil
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
	}
	firstStart, _ := goalWindow(days[0].Date, g, loc)
	_, lastEnd := goalWindow(days[len(days)-1].Date, g, loc)
	rows, err := query.Query(ctx, `SELECT starts_at,ends_at FROM focusflow.calendar_blocks WHERE user_id=$1 AND starts_at<$3 AND ends_at>$2`, userID, firstStart, lastEnd)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var blocks []timeSpan
	for rows.Next() {
		var span timeSpan
		if err := rows.Scan(&span.Start, &span.End); err != nil {
			return nil, nil, err
		}
		blocks = append(blocks, span)
	}
	for i := range days {
		windowStart, windowEnd := goalWindow(days[i].Date, g, loc)
		key := dayKey(days[i].Date)
		for _, block := range blocks {
			start := block.Start
			if start.Before(windowStart) {
				start = windowStart
			}
			end := block.End
			if end.After(windowEnd) {
				end = windowEnd
			}
			if end.After(start) {
				days[i].AlreadyPlanned += int64(end.Sub(start).Seconds())
				occupied[key] = append(occupied[key], timeSpan{Start: start, End: end})
			}
		}
	}
	return days, occupied, rows.Err()
}

func goalWindow(day time.Time, g goalResponse, loc *time.Location) (time.Time, time.Time) {
	startHour, startMinute := parseClock(g.PreferredStart)
	endHour, endMinute := parseClock(g.PreferredEnd)
	start := time.Date(day.Year(), day.Month(), day.Day(), startHour, startMinute, 0, 0, loc)
	end := time.Date(day.Year(), day.Month(), day.Day(), endHour, endMinute, 0, 0, loc)
	return start, end
}

func firstFreeSlot(windowStart, windowEnd time.Time, duration time.Duration, spans []timeSpan) (time.Time, bool) {
	cursor := windowStart
	for {
		if cursor.Add(duration).After(windowEnd) {
			return time.Time{}, false
		}
		var collision *timeSpan
		for i := range spans {
			if cursor.Before(spans[i].End) && cursor.Add(duration).After(spans[i].Start) {
				if collision == nil || spans[i].End.Before(collision.End) {
					collision = &spans[i]
				}
			}
		}
		if collision == nil {
			return cursor, true
		}
		cursor = collision.End
	}
}

func dayKey(day time.Time) string {
	return day.Format("2006-01-02")
}

type icsEvent struct {
	UID, Title string
	Start, End time.Time
}

func parseICS(raw string) []icsEvent {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	var items []icsEvent
	var cur *icsEvent
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "BEGIN:VEVENT":
			cur = &icsEvent{}
		case line == "END:VEVENT" && cur != nil:
			items = append(items, *cur)
			cur = nil
		case cur != nil && strings.HasPrefix(line, "UID:"):
			cur.UID = strings.TrimPrefix(line, "UID:")
		case cur != nil && strings.HasPrefix(line, "SUMMARY:"):
			cur.Title = strings.ReplaceAll(strings.TrimPrefix(line, "SUMMARY:"), `\n`, "\n")
		case cur != nil && strings.HasPrefix(line, "DTSTART"):
			if i := strings.Index(line, ":"); i >= 0 {
				cur.Start = parseICSTime(line[i+1:])
			}
		case cur != nil && strings.HasPrefix(line, "DTEND"):
			if i := strings.Index(line, ":"); i >= 0 {
				cur.End = parseICSTime(line[i+1:])
			}
		}
	}
	return items
}
func parseICSTime(v string) time.Time {
	for _, layout := range []string{"20060102T150405Z", "20060102T150405", "20060102"} {
		if t, e := time.Parse(layout, v); e == nil {
			return t
		}
	}
	return time.Time{}
}
func icsTime(t time.Time) string { return t.UTC().Format("20060102T150405Z") }
func escapeICS(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, ",", `\,`)
	v = strings.ReplaceAll(v, ";", `\;`)
	return strings.ReplaceAll(v, "\n", `\n`)
}
func parseClock(v string) (int, int) {
	p := strings.Split(v, ":")
	if len(p) != 2 {
		return 9, 0
	}
	h, _ := strconv.Atoi(p[0])
	m, _ := strconv.Atoi(p[1])
	return h, m
}
func greetingFor(now time.Time, tz string) string {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	h := now.In(loc).Hour()
	if h < 6 {
		return "Доброй ночи"
	}
	if h < 12 {
		return "Доброе утро"
	}
	if h < 18 {
		return "Добрый день"
	}
	return "Добрый вечер"
}
func randomBytes(n int) []byte { b := make([]byte, n); _, _ = rand.Read(b); return b }
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 12<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, r, 400, "invalid_json", "Не удалось прочитать запрос", nil)
		return false
	}
	return true
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string, fields map[string]string) {
	writeJSON(w, status, APIError{Code: code, Message: message, FieldErrors: fields, RequestID: middleware.GetReqID(r.Context())})
}
func writeDBError(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("database_error", "error", err, "request_id", middleware.GetReqID(r.Context()))
	writeError(w, r, 500, "internal_error", "Что-то пошло не так. Попробуйте ещё раз", nil)
}
