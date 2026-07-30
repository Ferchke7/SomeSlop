import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import * as Dialog from "@radix-ui/react-dialog";
import * as DropdownMenu from "@radix-ui/react-dropdown-menu";
import * as Progress from "@radix-ui/react-progress";
import * as Switch from "@radix-ui/react-switch";
import * as Tabs from "@radix-ui/react-tabs";
import FullCalendar from "@fullcalendar/react";
import dayGridPlugin from "@fullcalendar/daygrid";
import timeGridPlugin from "@fullcalendar/timegrid";
import listPlugin from "@fullcalendar/list";
import interactionPlugin from "@fullcalendar/interaction";
import ruLocale from "@fullcalendar/core/locales/ru";
import { useCreateBlockNote } from "@blocknote/react";
import { BlockNoteView } from "@blocknote/shadcn";
import {
  Area, AreaChart, Bar, BarChart, CartesianGrid, Cell, ResponsiveContainer,
  Tooltip as ChartTooltip, XAxis, YAxis
} from "recharts";
import {
  AlarmClock, Archive, ArrowRight, BarChart3, Bell, BookOpen, Brain,
  CalendarDays, Check, CheckCircle2, ChevronDown, ChevronLeft, ChevronRight,
  Circle, Clock3, Cloud, Command, Download, FilePlus2, FileText, Flame,
  FolderKanban, Inbox, Laptop, LayoutDashboard, ListChecks, Loader2, LogOut,
  Menu, Moon, MoreHorizontal, Pause, Play, Plus, RefreshCw, Repeat2, Search,
  Settings, Smartphone, Sparkles, Square, Sun, Tag, Target, TimerReset,
  Upload, WifiOff, X, Zap
} from "lucide-react";
import { Toaster, toast } from "sonner";
import { addDays, endOfMonth, format, isSameDay, startOfMonth } from "date-fns";
import { ru } from "date-fns/locale";

import { ankiInvoke, api, apiURL, APIError, contentHash, jsonBody } from "./api";
import type {
  Analytics, CalendarBlock, Card, Dashboard, Deck, Goal, Note,
  NotificationItem, Task, TimerState, User
} from "./types";

type View = "today" | "tasks" | "calendar" | "goals" | "focus" | "notes" | "review" | "analytics" | "settings";

const navItems: Array<{ id: View; label: string; icon: typeof LayoutDashboard }> = [
  { id: "today", label: "Сегодня", icon: LayoutDashboard },
  { id: "tasks", label: "Задачи", icon: ListChecks },
  { id: "calendar", label: "Календарь", icon: CalendarDays },
  { id: "goals", label: "Цели", icon: Target },
  { id: "focus", label: "Фокус", icon: TimerReset },
  { id: "notes", label: "Заметки", icon: FileText },
  { id: "review", label: "Повторение", icon: Brain },
  { id: "analytics", label: "Аналитика", icon: BarChart3 },
  { id: "settings", label: "Настройки", icon: Settings }
];

interface Workspace {
  dashboard: Dashboard;
  tasks: Task[];
  goals: Goal[];
  blocks: CalendarBlock[];
  timer: TimerState;
  notes: Note[];
  decks: Deck[];
  cards: Card[];
  analytics: Analytics;
  notifications: NotificationItem[];
}

const emptyTimer: TimerState = {
  mode: "stopwatch", status: "idle", accumulatedSeconds: 0, elapsedSeconds: 0, version: 0
};

export function App() {
  const [user, setUser] = useState<User | null>(null);
  const [workspace, setWorkspace] = useState<Workspace | null>(null);
  const [loading, setLoading] = useState(true);
  const [view, setView] = useState<View>("today");
  const [quickTaskOpen, setQuickTaskOpen] = useState(false);
  const [commandOpen, setCommandOpen] = useState(false);
  const [mobileMenu, setMobileMenu] = useState(false);
  const [theme, setTheme] = useState<"light" | "dark">(() => {
    const saved = localStorage.getItem("focusflow-theme");
    if (saved === "dark" || saved === "light") return saved;
    return matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
  });

  const loadWorkspace = useCallback(async () => {
    const from = startOfMonth(addDays(new Date(), -31)).toISOString();
    const to = endOfMonth(addDays(new Date(), 62)).toISOString();
    const [
      dashboard, tasks, goals, blocks, timer, notes, decks, cards, analytics, notifications
    ] = await Promise.all([
      api<Dashboard>("/dashboard"),
      api<Task[]>("/tasks/"),
      api<Goal[]>("/goals/"),
      api<CalendarBlock[]>(`/calendar/?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`),
      api<TimerState>("/timer/"),
      api<Note[]>("/notes/"),
      api<Deck[]>("/decks/"),
      api<Card[]>("/cards/due"),
      api<Analytics>("/analytics"),
      api<NotificationItem[]>("/notifications/")
    ]);
    setWorkspace({ dashboard, tasks, goals, blocks, timer, notes, decks, cards, analytics, notifications });
  }, []);

  useEffect(() => {
    document.documentElement.dataset.theme = theme;
    localStorage.setItem("focusflow-theme", theme);
  }, [theme]);

  useEffect(() => {
    void api<User>("/me")
      .then(async (me) => { setUser(me); await loadWorkspace(); })
      .catch((error) => {
        if (!(error instanceof APIError) || error.status !== 401) toast.error(error.message);
      })
      .finally(() => setLoading(false));
  }, [loadWorkspace]);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement;
      const typing = target.matches("input,textarea,[contenteditable=true]");
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
        event.preventDefault(); setCommandOpen((open) => !open);
      } else if (!typing && event.key.toLowerCase() === "q") {
        event.preventDefault(); setQuickTaskOpen(true);
      }
    };
    addEventListener("keydown", onKey);
    return () => removeEventListener("keydown", onKey);
  }, []);

  const onAuthenticated = async (me: User) => {
    setUser(me);
    setLoading(true);
    try { await loadWorkspace(); } finally { setLoading(false); }
  };

  const mutate = async (action: () => Promise<unknown>, success?: string) => {
    try {
      await action();
      if (success) toast.success(success);
      await loadWorkspace();
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Не удалось сохранить изменения");
      throw error;
    }
  };

  if (loading) return <LoadingScreen />;
  if (!user) return (
    <>
      <Toaster position="top-center" richColors />
      <AuthScreen onAuthenticated={onAuthenticated} />
    </>
  );
  if (!workspace) return <ErrorScreen onRetry={() => void loadWorkspace()} />;

  const navigate = (next: View) => {
    setView(next); setMobileMenu(false); setCommandOpen(false);
  };

  const currentLabel = navItems.find((item) => item.id === view)?.label ?? "FocusFlow";
  const viewProps = { user, workspace, setWorkspace, reload: loadWorkspace, mutate, navigate, theme, setTheme };

  return (
    <div className="app-shell">
      <Toaster position="top-center" richColors closeButton />
      <aside className={`sidebar ${mobileMenu ? "is-open" : ""}`}>
        <div className="brand">
          <span className="brand-mark">F<span /></span>
          <span className="brand-copy"><strong>FocusFlow</strong><small>личная система</small></span>
          <button className="icon-button sidebar-close" onClick={() => setMobileMenu(false)} aria-label="Закрыть меню"><X size={18} /></button>
        </div>
        <button className="quick-add" onClick={() => setQuickTaskOpen(true)}>
          <Plus size={18} /> Быстро добавить <kbd>Q</kbd>
        </button>
        <nav className="primary-nav" aria-label="Основная навигация">
          {navItems.map(({ id, label, icon: Icon }) => (
            <button key={id} className={view === id ? "active" : ""} onClick={() => navigate(id)}>
              <Icon size={18} strokeWidth={1.8} /><span>{label}</span>
              {id === "tasks" && workspace.dashboard.openTasks > 0 && <em>{workspace.dashboard.openTasks}</em>}
              {id === "review" && workspace.dashboard.dueCards > 0 && <em className="green">{workspace.dashboard.dueCards}</em>}
            </button>
          ))}
        </nav>
        <div className="sidebar-projects">
          <div className="sidebar-caption"><span>Пространства</span><Plus size={14} /></div>
          <button><span className="project-dot indigo" /> Развитие <MoreHorizontal size={15} /></button>
          <button><span className="project-dot coral" /> Личное <MoreHorizontal size={15} /></button>
        </div>
        <div className="sidebar-footer">
          <div className="avatar">{initials(user.displayName)}</div>
          <div><strong>{user.displayName}</strong><small>{user.email}</small></div>
          <DropdownMenu.Root>
            <DropdownMenu.Trigger asChild><button className="icon-button"><MoreHorizontal size={17} /></button></DropdownMenu.Trigger>
            <DropdownMenu.Portal>
              <DropdownMenu.Content className="dropdown" align="end">
                <DropdownMenu.Item onSelect={() => navigate("settings")}><Settings size={15} /> Настройки</DropdownMenu.Item>
                <DropdownMenu.Item onSelect={() => void api("/auth/logout", { method: "POST" }).then(() => location.reload())}><LogOut size={15} /> Выйти</DropdownMenu.Item>
              </DropdownMenu.Content>
            </DropdownMenu.Portal>
          </DropdownMenu.Root>
        </div>
      </aside>

      {mobileMenu && <button className="sidebar-scrim" onClick={() => setMobileMenu(false)} aria-label="Закрыть меню" />}

      <main className="main-area">
        <header className="topbar">
          <button className="icon-button mobile-menu-button" onClick={() => setMobileMenu(true)} aria-label="Открыть меню"><Menu size={20} /></button>
          <div className="mobile-title">{currentLabel}</div>
          <button className="search-trigger" onClick={() => setCommandOpen(true)}><Search size={17} /><span>Найти что угодно…</span><kbd>⌘ K</kbd></button>
          <div className="top-actions">
            <button className="icon-button" onClick={() => setTheme(theme === "light" ? "dark" : "light")} aria-label="Переключить тему">{theme === "light" ? <Moon size={18} /> : <Sun size={18} />}</button>
            <DropdownMenu.Root>
              <DropdownMenu.Trigger asChild>
                <button className="icon-button notification-button" aria-label="Уведомления"><Bell size={18} />{workspace.notifications.some((item) => item.status === "sent") && <i />}</button>
              </DropdownMenu.Trigger>
              <DropdownMenu.Portal>
                <DropdownMenu.Content className="dropdown notifications-dropdown" align="end">
                  <div className="dropdown-heading">Уведомления</div>
                  {workspace.notifications.length ? workspace.notifications.slice(0, 5).map((item) => (
                    <DropdownMenu.Item key={item.id} className="notification-row">
                      <span className="notification-icon"><Bell size={15} /></span>
                      <span><strong>{item.title}</strong><small>{relativeDate(item.remindAt)}</small></span>
                    </DropdownMenu.Item>
                  )) : <div className="empty-dropdown">Пока всё спокойно</div>}
                </DropdownMenu.Content>
              </DropdownMenu.Portal>
            </DropdownMenu.Root>
            <button className="top-avatar" onClick={() => navigate("settings")}>{initials(user.displayName)}</button>
          </div>
        </header>

        <div className="view-stage">
          {view === "today" && <TodayView {...viewProps} />}
          {view === "tasks" && <TasksView {...viewProps} onAdd={() => setQuickTaskOpen(true)} />}
          {view === "calendar" && <CalendarView {...viewProps} />}
          {view === "goals" && <GoalsView {...viewProps} />}
          {view === "focus" && <FocusView {...viewProps} />}
          {view === "notes" && <NotesView {...viewProps} />}
          {view === "review" && <ReviewView {...viewProps} />}
          {view === "analytics" && <AnalyticsView {...viewProps} />}
          {view === "settings" && <SettingsView {...viewProps} />}
        </div>
      </main>

      <nav className="mobile-nav" aria-label="Мобильная навигация">
        {navItems.slice(0, 8).map(({ id, label, icon: Icon }) => (
          <button key={id} className={view === id ? "active" : ""} onClick={() => navigate(id)}>
            <Icon size={19} /><span>{label}</span>
          </button>
        ))}
      </nav>

      <QuickTaskDialog
        open={quickTaskOpen}
        onOpenChange={setQuickTaskOpen}
        goals={workspace.goals}
        onCreate={(input) => mutate(() => api("/tasks/", { method: "POST", ...jsonBody(input) }), "Задача добавлена")}
      />
      <CommandPalette open={commandOpen} onOpenChange={setCommandOpen} workspace={workspace} navigate={navigate} onQuickTask={() => { setCommandOpen(false); setQuickTaskOpen(true); }} />
    </div>
  );
}

type ViewProps = {
  user: User;
  workspace: Workspace;
  setWorkspace: React.Dispatch<React.SetStateAction<Workspace | null>>;
  reload: () => Promise<void>;
  mutate: (action: () => Promise<unknown>, success?: string) => Promise<void>;
  navigate: (view: View) => void;
  theme: "light" | "dark";
  setTheme: (theme: "light" | "dark") => void;
};

function TodayView({ user, workspace, mutate, navigate }: ViewProps) {
  const todayBlocks = workspace.blocks.filter((block) => isSameDay(new Date(block.startsAt), new Date())).sort((a, b) => +new Date(a.startsAt) - +new Date(b.startsAt));
  const goal = workspace.goals[0];
  const focusData = normalizedSeries(workspace.analytics.focusSeries, 7);
  return (
    <div className="page page-today">
      <section className="today-heading">
        <div>
          <div className="eyebrow">{format(new Date(), "EEEE, d MMMM", { locale: ru })}</div>
          <h1>{workspace.dashboard.greeting}, {firstName(user.displayName)} <span className="wave">✦</span></h1>
          <p>Вот главное на сегодня. Двигайтесь спокойно, по одному блоку за раз.</p>
        </div>
        <button className="secondary-button date-button" onClick={() => navigate("calendar")}><CalendarDays size={17} /> {format(new Date(), "d MMM")} <ChevronDown size={15} /></button>
      </section>

      <section className="summary-grid">
        <button className="summary-card indigo-card" onClick={() => navigate("tasks")}>
          <span className="summary-icon"><ListChecks size={20} /></span><span><small>Открытые задачи</small><strong>{workspace.dashboard.openTasks}</strong></span><ArrowRight size={18} />
        </button>
        <button className="summary-card green-card" onClick={() => navigate("focus")}>
          <span className="summary-icon"><Clock3 size={20} /></span><span><small>Фокус сегодня</small><strong>{formatDuration(workspace.dashboard.trackedTodaySeconds)}</strong></span><ArrowRight size={18} />
        </button>
        <button className="summary-card amber-card" onClick={() => navigate("review")}>
          <span className="summary-icon"><Brain size={20} /></span><span><small>К повторению</small><strong>{workspace.dashboard.dueCards}</strong></span><ArrowRight size={18} />
        </button>
      </section>

      <div className="today-layout">
        <section className="panel schedule-panel">
          <PanelHeading title="План дня" subtitle={`${todayBlocks.length} блоков · ${formatDuration(todayBlocks.reduce((sum, block) => sum + (+new Date(block.endsAt) - +new Date(block.startsAt)) / 1000, 0))}`} action={<button onClick={() => navigate("calendar")}>Календарь <ArrowRight size={14} /></button>} />
          <div className="timeline">
            {todayBlocks.length ? todayBlocks.map((block, index) => (
              <div className="timeline-row" key={block.id}>
                <time>{format(new Date(block.startsAt), "HH:mm")}</time>
                <span className="timeline-line"><i style={{ background: block.color }} />{index < todayBlocks.length - 1 && <b />}</span>
                <button className="timeline-event" onClick={() => navigate("focus")}>
                  <span><strong>{block.title}</strong><small>{format(new Date(block.startsAt), "HH:mm")}–{format(new Date(block.endsAt), "HH:mm")} · {block.source === "planner" ? "автоплан" : "календарь"}</small></span>
                  <Play size={15} fill="currentColor" />
                </button>
              </div>
            )) : <EmptyState compact icon={CalendarDays} title="День пока свободен" text="Добавьте блок или создайте план из месячной цели." action={<button className="text-button" onClick={() => navigate("calendar")}>Открыть календарь</button>} />}
          </div>
        </section>

        <div className="today-side">
          <TimerCard workspace={workspace} mutate={mutate} onOpen={() => navigate("focus")} />
          {goal && <GoalMiniCard goal={goal} onOpen={() => navigate("goals")} onPlan={() => mutate(() => api(`/goals/${goal.id}/plan`, { method: "POST" }), "План обновлён")} />}
        </div>
      </div>

      <div className="today-bottom">
        <section className="panel due-panel">
          <PanelHeading title="Следующие задачи" action={<button onClick={() => navigate("tasks")}>Все задачи <ArrowRight size={14} /></button>} />
          <div className="compact-task-list">
            {workspace.tasks.filter((task) => task.status !== "done").slice(0, 4).map((task) => (
              <TaskRow key={task.id} task={task} compact onToggle={() => mutate(() => api(`/tasks/${task.id}`, { method: "PATCH", ...jsonBody({ status: "done", version: task.version }) }))} />
            ))}
          </div>
        </section>
        <section className="panel chart-panel">
          <PanelHeading title="Ритм фокуса" subtitle="Последние 7 дней" />
          <div className="mini-chart">
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={focusData}>
                <defs><linearGradient id="focusGradient" x1="0" y1="0" x2="0" y2="1"><stop offset="0%" stopColor="#6f6fe6" stopOpacity=".35" /><stop offset="100%" stopColor="#6f6fe6" stopOpacity="0" /></linearGradient></defs>
                <CartesianGrid vertical={false} stroke="var(--line)" strokeDasharray="3 4" />
                <XAxis dataKey="label" axisLine={false} tickLine={false} tick={{ fill: "var(--muted)", fontSize: 11 }} />
                <ChartTooltip content={<FocusTooltip />} />
                <Area type="monotone" dataKey="hours" stroke="#6464dc" strokeWidth={2.5} fill="url(#focusGradient)" />
              </AreaChart>
            </ResponsiveContainer>
          </div>
        </section>
      </div>
    </div>
  );
}

function TasksView({ workspace, mutate, navigate, onAdd }: ViewProps & { onAdd: () => void }) {
  const [filter, setFilter] = useState<"all" | "today" | "upcoming" | "done">("all");
  const [query, setQuery] = useState("");
  const visible = workspace.tasks.filter((task) => {
    if (query && !task.title.toLowerCase().includes(query.toLowerCase())) return false;
    if (filter === "done") return task.status === "done";
    if (task.status === "done") return false;
    if (filter === "today") return task.dueAt && isSameDay(new Date(task.dueAt), new Date());
    if (filter === "upcoming") return task.dueAt && new Date(task.dueAt) > new Date();
    return true;
  });
  return (
    <div className="page">
      <PageHeader eyebrow="Организация" title="Задачи" description="Соберите входящие, выберите главное и назначьте время." actions={<button className="primary-button" onClick={onAdd}><Plus size={17} /> Новая задача</button>} />
      <div className="toolbar">
        <Tabs.Root value={filter} onValueChange={(value) => setFilter(value as typeof filter)}>
          <Tabs.List className="segmented">
            <Tabs.Trigger value="all"><Inbox size={15} /> Входящие</Tabs.Trigger>
            <Tabs.Trigger value="today">Сегодня</Tabs.Trigger>
            <Tabs.Trigger value="upcoming">Предстоящие</Tabs.Trigger>
            <Tabs.Trigger value="done">Готово</Tabs.Trigger>
          </Tabs.List>
        </Tabs.Root>
        <label className="inline-search"><Search size={16} /><input placeholder="Фильтр задач" value={query} onChange={(event) => setQuery(event.target.value)} /></label>
      </div>
      <section className="panel tasks-panel">
        <div className="list-heading">
          <div><span>{filter === "today" ? "На сегодня" : filter === "upcoming" ? "Предстоящие" : filter === "done" ? "Завершённые" : "Все задачи"}</span><em>{visible.length}</em></div>
          <button className="icon-button"><MoreHorizontal size={18} /></button>
        </div>
        <div className="task-list">
          {visible.length ? visible.map((task) => (
            <TaskRow
              key={task.id}
              task={task}
              onToggle={() => mutate(() => api(`/tasks/${task.id}`, { method: "PATCH", ...jsonBody({ status: task.status === "done" ? "todo" : "done", version: task.version }) }), task.status === "done" ? "Задача возвращена" : "Готово")}
              onStart={() => mutate(() => api("/timer/start", { method: "POST", ...jsonBody({ taskId: task.id, goalId: task.goalId, projectId: task.projectId, mode: "stopwatch" }) })).then(() => navigate("focus"))}
              onArchive={() => mutate(() => api(`/tasks/${task.id}`, { method: "DELETE" }), "Задача архивирована")}
            />
          )) : <EmptyState icon={CheckCircle2} title="Здесь всё чисто" text="Добавьте следующую задачу или переключите фильтр." action={<button className="primary-button" onClick={onAdd}><Plus size={16} /> Добавить задачу</button>} />}
        </div>
      </section>
    </div>
  );
}

function CalendarView({ workspace, mutate }: ViewProps) {
  const [blockDialog, setBlockDialog] = useState<{ open: boolean; start?: Date; end?: Date }>({ open: false });
  const uploadRef = useRef<HTMLInputElement>(null);
  const events = workspace.blocks.map((block) => ({
    id: block.id, title: block.title, start: block.startsAt, end: block.endsAt,
    backgroundColor: block.color, borderColor: block.color,
    editable: !block.locked, extendedProps: block
  }));
  const updateEvent = (info: any) => {
    if (!info.event.start || !info.event.end) return;
    void mutate(() => api(`/calendar/${info.event.id}`, {
      method: "PATCH", ...jsonBody({ startsAt: info.event.start, endsAt: info.event.end, version: info.event.extendedProps.version })
    })).catch(() => info.revert());
  };
  const importICS = async (file?: File) => {
    if (!file) return;
    try {
      const result = await fetch(apiURL("/calendar/import.ics"), { method: "POST", body: file, credentials: "include", headers: { "Content-Type": "text/calendar" } });
      if (!result.ok) throw new Error("Не удалось импортировать календарь");
      toast.success("События импортированы");
      await mutate(async () => undefined);
    } catch (error) { toast.error(error instanceof Error ? error.message : "Ошибка импорта"); }
  };
  return (
    <div className="page calendar-page">
      <PageHeader eyebrow="Время" title="Календарь" description="Перетаскивайте блоки, изменяйте длительность и оставляйте место для жизни." actions={<>
        <button className="secondary-button" onClick={() => uploadRef.current?.click()}><Upload size={16} /> ICS</button>
        <a className="secondary-button" href={apiURL("/calendar/export.ics")}><Download size={16} /> Экспорт</a>
        <button className="primary-button" onClick={() => setBlockDialog({ open: true })}><Plus size={17} /> Блок времени</button>
        <input ref={uploadRef} hidden type="file" accept=".ics,text/calendar" onChange={(event) => void importICS(event.target.files?.[0])} />
      </>} />
      <section className="panel calendar-panel">
        <FullCalendar
          plugins={[dayGridPlugin, timeGridPlugin, listPlugin, interactionPlugin]}
          locale={ruLocale}
          initialView={window.innerWidth < 720 ? "listWeek" : "timeGridWeek"}
          headerToolbar={{ left: "prev,next today", center: "title", right: "dayGridMonth,timeGridWeek,timeGridDay,listWeek" }}
          buttonText={{ today: "Сегодня", month: "Месяц", week: "Неделя", day: "День", list: "Список" }}
          events={events}
          editable
          selectable
          nowIndicator
          firstDay={1}
          slotMinTime="06:00:00"
          slotMaxTime="23:00:00"
          slotDuration="00:30:00"
          allDaySlot={false}
          height="auto"
          select={(selection) => setBlockDialog({ open: true, start: selection.start, end: selection.end })}
          eventDrop={updateEvent}
          eventResize={updateEvent}
          eventContent={(info) => (
            <div className="calendar-event-content"><strong>{info.event.title}</strong><span>{info.timeText}</span>{info.event.extendedProps.source === "planner" && <Sparkles size={11} />}</div>
          )}
        />
      </section>
      <CalendarBlockDialog open={blockDialog.open} start={blockDialog.start} end={blockDialog.end} goals={workspace.goals} onOpenChange={(open) => setBlockDialog({ open })} onCreate={(input) => mutate(() => api("/calendar/", { method: "POST", ...jsonBody(input) }), "Блок добавлен")} />
    </div>
  );
}

function GoalsView({ workspace, mutate }: ViewProps) {
  const [open, setOpen] = useState(false);
  return (
    <div className="page">
      <PageHeader eyebrow="Направление" title="Месячные цели" description="Задайте объём, а FocusFlow рассчитает темп и аккуратно разложит работу по календарю." actions={<button className="primary-button" onClick={() => setOpen(true)}><Plus size={17} /> Новая цель</button>} />
      <div className="goals-grid">
        {workspace.goals.map((goal) => <GoalCard key={goal.id} goal={goal} onPlan={() => mutate(() => api(`/goals/${goal.id}/plan`, { method: "POST" }), "Календарь перепланирован")} />)}
        <button className="new-goal-card" onClick={() => setOpen(true)}><span><Plus size={24} /></span><strong>Создать цель</strong><small>Например, 120 часов практики за месяц</small></button>
      </div>
      <section className="panel planner-explainer">
        <div className="explainer-icon"><Sparkles size={22} /></div>
        <div><h3>Как работает умное планирование</h3><p>Мы вычитаем уже отслеженное время, делим остаток на доступные дни и создаём фокус-сессии только в будущих свободных окнах. Закреплённые блоки остаются на месте.</p></div>
        <div className="explainer-steps"><span><b>1</b> Цель</span><i /><span><b>2</b> Свободное время</span><i /><span><b>3</b> Фокус-блоки</span></div>
      </section>
      <GoalDialog open={open} onOpenChange={setOpen} onCreate={(input) => mutate(() => api("/goals/", { method: "POST", ...jsonBody(input) }), "Цель создана")} />
    </div>
  );
}

function FocusView({ workspace, mutate }: ViewProps) {
  const [selectedTask, setSelectedTask] = useState(workspace.timer.taskId ?? workspace.tasks.find((task) => task.status !== "done")?.id ?? "");
  const [mode, setMode] = useState<"stopwatch" | "pomodoro">(workspace.timer.mode || "stopwatch");
  const elapsed = useLiveElapsed(workspace.timer);
  const target = workspace.timer.targetSeconds ?? (mode === "pomodoro" ? 25 * 60 : 0);
  const shown = mode === "pomodoro" && target ? Math.max(target - elapsed, 0) : elapsed;
  const activeTask = workspace.tasks.find((task) => task.id === (workspace.timer.taskId || selectedTask));
  const start = () => mutate(() => api("/timer/start", { method: "POST", ...jsonBody({
    taskId: selectedTask || null, goalId: activeTask?.goalId, projectId: activeTask?.projectId,
    mode, targetSeconds: mode === "pomodoro" ? 25 * 60 : null
  }) }), "Фокус начат");
  return (
    <div className="page focus-page">
      <PageHeader eyebrow="Глубокая работа" title="Фокус" description="Один таймер, одна задача, одно спокойное движение вперёд." />
      <div className="focus-layout">
        <section className="panel focus-timer-panel">
          <Tabs.Root value={mode} onValueChange={(value) => workspace.timer.status === "idle" && setMode(value as typeof mode)}>
            <Tabs.List className="segmented timer-mode">
              <Tabs.Trigger value="stopwatch">Секундомер</Tabs.Trigger>
              <Tabs.Trigger value="pomodoro">Pomodoro · 25</Tabs.Trigger>
            </Tabs.List>
          </Tabs.Root>
          <label className="focus-task-select">
            <span>Сейчас работаю над</span>
            <select value={workspace.timer.taskId ?? selectedTask} disabled={workspace.timer.status !== "idle"} onChange={(event) => setSelectedTask(event.target.value)}>
              <option value="">Без задачи</option>
              {workspace.tasks.filter((task) => task.status !== "done").map((task) => <option key={task.id} value={task.id}>{task.title}</option>)}
            </select>
          </label>
          <div className={`timer-orbit ${workspace.timer.status === "running" ? "is-running" : ""}`} style={{ "--timer-progress": `${target ? Math.min(elapsed / target * 360, 360) : 285}deg` } as React.CSSProperties}>
            <div className="timer-orbit-inner">
              <small>{workspace.timer.status === "running" ? "В ФОКУСЕ" : workspace.timer.status === "paused" ? "ПАУЗА" : "ГОТОВЫ?"}</small>
              <strong>{clock(shown)}</strong>
              <span>{activeTask?.title || "Выберите задачу"}</span>
            </div>
          </div>
          <div className="timer-controls">
            {workspace.timer.status === "idle" && <button className="focus-play" onClick={() => void start()}><Play size={25} fill="currentColor" /></button>}
            {workspace.timer.status === "running" && <>
              <button className="timer-side-button" onClick={() => void mutate(() => api("/timer/stop", { method: "POST" }), "Сессия сохранена")}><Square size={18} fill="currentColor" /></button>
              <button className="focus-play" onClick={() => void mutate(() => api("/timer/pause", { method: "POST" }))}><Pause size={25} fill="currentColor" /></button>
            </>}
            {workspace.timer.status === "paused" && <>
              <button className="timer-side-button" onClick={() => void mutate(() => api("/timer/stop", { method: "POST" }), "Сессия сохранена")}><Square size={18} fill="currentColor" /></button>
              <button className="focus-play" onClick={() => void mutate(() => api("/timer/resume", { method: "POST" }))}><Play size={25} fill="currentColor" /></button>
            </>}
          </div>
          <p className="timer-hint">Таймер продолжит идти при обновлении страницы и синхронизируется между устройствами.</p>
        </section>
        <aside className="focus-sidebar">
          <section className="panel intention-card">
            <span className="panel-kicker"><Zap size={14} /> Намерение сессии</span>
            <textarea placeholder="Что должно быть готово к концу этой сессии?" defaultValue={activeTask ? `Продвинуть задачу «${activeTask.title}» до понятного результата.` : ""} />
          </section>
          <section className="panel session-stats">
            <PanelHeading title="Сегодня" />
            <div className="stat-pair"><span><strong>{formatDuration(workspace.dashboard.trackedTodaySeconds)}</strong><small>в фокусе</small></span><span><strong>{workspace.dashboard.completedToday}</strong><small>задач готово</small></span></div>
            <div className="streak"><Flame size={18} /><div><strong>Хороший ритм</strong><small>Сохраните ещё одну сессию сегодня</small></div></div>
          </section>
          <section className="panel focus-tips">
            <PanelHeading title="Перед стартом" />
            {["Закройте лишние вкладки", "Положите телефон экраном вниз", "Сформулируйте один результат"].map((tip) => <label key={tip}><CheckCircle2 size={16} /> {tip}</label>)}
          </section>
        </aside>
      </div>
    </div>
  );
}

function NotesView({ workspace, setWorkspace, mutate }: ViewProps) {
  const [selectedID, setSelectedID] = useState(workspace.notes[0]?.id ?? "");
  const [query, setQuery] = useState("");
  const selected = workspace.notes.find((note) => note.id === selectedID) ?? workspace.notes[0];
  const deck = workspace.decks[0];
  const filtered = workspace.notes.filter((note) => note.title.toLowerCase().includes(query.toLowerCase()) || note.plainText.toLowerCase().includes(query.toLowerCase()));
  const createNote = async () => {
    let created: { id: string };
    await mutate(async () => {
      created = await api<{ id: string }>("/notes/", { method: "POST", ...jsonBody({ title: "Без названия" }) });
    }, "Заметка создана");
    // loadWorkspace has already refreshed; the new note will be available on the next render.
  };
  const updateLocal = (id: string, patch: Partial<Note>) => setWorkspace((current) => current ? ({ ...current, notes: current.notes.map((note) => note.id === id ? { ...note, ...patch } : note) }) : current);
  const convertToCard = async (text: string) => {
    if (!deck) { toast.error("Сначала создайте колоду"); return; }
    await mutate(() => api("/cards/", { method: "POST", ...jsonBody({ deckId: deck.id, front: text || selected?.title, back: selected?.plainText || "", cardType: "basic", sourceNoteId: selected?.id }) }), "Карточка создана");
  };
  return (
    <div className="page notes-page">
      <PageHeader eyebrow="Знания" title="Заметки" description="Свободно пишите, связывайте идеи и превращайте важное в карточки." actions={<button className="primary-button" onClick={() => void createNote()}><FilePlus2 size={17} /> Новая страница</button>} />
      <div className="notes-workspace panel">
        <aside className="notes-tree">
          <label className="inline-search"><Search size={15} /><input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Поиск заметок" /></label>
          <div className="notes-tree-caption">Личные страницы</div>
          <div className="notes-list">
            {filtered.map((note) => <button key={note.id} className={selected?.id === note.id ? "active" : ""} onClick={() => setSelectedID(note.id)}><FileText size={16} /><span>{note.title}</span><MoreHorizontal size={14} /></button>)}
          </div>
          <button className="new-page-link" onClick={() => void createNote()}><Plus size={15} /> Добавить страницу</button>
        </aside>
        <section className="editor-pane">
          {selected ? <NoteEditor key={selected.id} note={selected} theme={document.documentElement.dataset.theme === "dark" ? "dark" : "light"} onLocal={(patch) => updateLocal(selected.id, patch)} onSaved={(version) => updateLocal(selected.id, { version })} onConvert={convertToCard} onArchive={() => mutate(() => api(`/notes/${selected.id}`, { method: "DELETE" }), "Заметка архивирована")} /> : <EmptyState icon={FileText} title="Создайте первую заметку" text="Здесь появится удобный блочный редактор." />}
        </section>
      </div>
    </div>
  );
}

function NoteEditor({ note, theme, onLocal, onSaved, onConvert, onArchive }: { note: Note; theme: "light" | "dark"; onLocal: (patch: Partial<Note>) => void; onSaved: (version: number) => void; onConvert: (text: string) => Promise<void>; onArchive: () => Promise<void> }) {
  const editor = useCreateBlockNote({
    initialContent: note.content?.length ? note.content as never : undefined,
    uploadFile: async (file) => {
      const form = new FormData();
      form.append("file", file);
      const response = await fetch(apiURL(`/notes/${note.id}/attachments`), { method: "POST", body: form, credentials: "include" });
      if (!response.ok) throw new Error("Не удалось загрузить вложение");
      const result = await response.json() as { url: string };
      return result.url;
    }
  });
  const saveTimer = useRef<number | undefined>(undefined);
  const versionRef = useRef(note.version);
  const [title, setTitle] = useState(note.title);
  const save = (nextTitle = title) => {
    const content = editor.document;
    const plainText = content.map((block) => {
      const blockContent = block.content as unknown;
      if (!Array.isArray(blockContent)) return "";
      return blockContent.map((part: unknown) => {
        if (part && typeof part === "object" && "text" in part) return String((part as { text: unknown }).text);
        return "";
      }).join("");
    }).filter(Boolean).join("\n");
    onLocal({ title: nextTitle, content: content as unknown[], plainText });
    clearTimeout(saveTimer.current);
    saveTimer.current = window.setTimeout(async () => {
      try {
        const response = await api<{ version: number }>(`/notes/${note.id}`, { method: "PATCH", ...jsonBody({ title: nextTitle, content, plainText, version: versionRef.current }) });
        versionRef.current = response.version; onSaved(response.version);
      } catch (error) { toast.error(error instanceof Error ? error.message : "Не удалось сохранить заметку"); }
    }, 700);
  };
  useEffect(() => () => clearTimeout(saveTimer.current), []);
  const createCard = async () => {
    const selectedText = (editor as unknown as { getSelectedText?: () => string }).getSelectedText?.();
    await onConvert(selectedText?.trim() || title);
  };
  return (
    <>
      <div className="editor-toolbar">
        <span><Cloud size={15} /> Сохранено</span>
        <div>
          <button className="secondary-button small" onClick={() => void createCard()}><Brain size={14} /> В карточку</button>
          <DropdownMenu.Root>
            <DropdownMenu.Trigger asChild><button className="icon-button"><MoreHorizontal size={18} /></button></DropdownMenu.Trigger>
            <DropdownMenu.Portal><DropdownMenu.Content className="dropdown" align="end"><DropdownMenu.Item className="danger" onSelect={() => void onArchive()}><Archive size={15} /> Архивировать</DropdownMenu.Item></DropdownMenu.Content></DropdownMenu.Portal>
          </DropdownMenu.Root>
        </div>
      </div>
      <div className="editor-document">
        <input className="note-title-input" value={title} onChange={(event) => { setTitle(event.target.value); save(event.target.value); }} placeholder="Без названия" />
        <BlockNoteView editor={editor} theme={theme} onChange={() => save()} />
      </div>
    </>
  );
}

function ReviewView({ workspace, setWorkspace, mutate }: ViewProps) {
  const [revealed, setRevealed] = useState(false);
  const [startedAt, setStartedAt] = useState(Date.now());
  const [deckFilter, setDeckFilter] = useState("all");
  const cards = workspace.cards.filter((card) => deckFilter === "all" || card.deckId === deckFilter);
  const card = cards[0];
  const answer = async (rating: number) => {
    if (!card) return;
    try {
      await api(`/cards/${card.id}/review`, { method: "POST", ...jsonBody({ rating, durationMs: Date.now() - startedAt }) });
      setWorkspace((current) => current ? ({ ...current, cards: current.cards.filter((item) => item.id !== card.id), dashboard: { ...current.dashboard, dueCards: Math.max(current.dashboard.dueCards - 1, 0) } }) : current);
      setRevealed(false); setStartedAt(Date.now());
    } catch (error) { toast.error(error instanceof Error ? error.message : "Не удалось сохранить ответ"); }
  };
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (["INPUT", "TEXTAREA"].includes((event.target as HTMLElement).tagName)) return;
      if (!revealed && event.code === "Space") { event.preventDefault(); setRevealed(true); }
      if (revealed && ["1", "2", "3", "4"].includes(event.key)) void answer(Number(event.key));
    };
    addEventListener("keydown", onKey); return () => removeEventListener("keydown", onKey);
  });
  return (
    <div className="page review-page">
      <PageHeader eyebrow="Память" title="Повторение" description="Показываем только то, что пора вспомнить — без лишних повторов." actions={<select className="compact-select" value={deckFilter} onChange={(event) => setDeckFilter(event.target.value)}><option value="all">Все колоды</option>{workspace.decks.map((deck) => <option key={deck.id} value={deck.id}>{deck.name}</option>)}</select>} />
      <div className="review-progress">
        <span><Brain size={16} /> Осталось <strong>{cards.length}</strong></span>
        <Progress.Root className="progress-root" value={Math.max(workspace.dashboard.dueCards - cards.length, 0) / Math.max(workspace.dashboard.dueCards, 1) * 100}><Progress.Indicator className="progress-indicator green" style={{ transform: `translateX(-${100 - (Math.max(workspace.dashboard.dueCards - cards.length, 0) / Math.max(workspace.dashboard.dueCards, 1) * 100)}%)` }} /></Progress.Root>
        <span>Удержание <strong>{workspace.analytics.retention || 90}%</strong></span>
      </div>
      {card ? (
        <div className="review-stage">
          <section className={`review-card ${revealed ? "revealed" : ""}`} onClick={() => !revealed && setRevealed(true)}>
            <div className="review-card-meta"><span style={{ background: workspace.decks.find((deck) => deck.id === card.deckId)?.color }}>{card.deckName}</span><small>{card.cardType === "cloze" ? "Cloze" : "Basic"}</small></div>
            <div className="review-question"><small>ВОПРОС</small><h2>{plainCard(card.front)}</h2></div>
            {revealed ? <div className="review-answer"><small>ОТВЕТ</small><p>{plainCard(card.back)}</p></div> : <button className="reveal-button">Показать ответ <kbd>Пробел</kbd></button>}
          </section>
          {revealed && <div className="rating-grid">
            {[
              { rating: 1, label: "Снова", key: "again", tone: "again" },
              { rating: 2, label: "Трудно", key: "hard", tone: "hard" },
              { rating: 3, label: "Хорошо", key: "good", tone: "good" },
              { rating: 4, label: "Легко", key: "easy", tone: "easy" }
            ].map((item) => <button className={item.tone} key={item.rating} onClick={() => void answer(item.rating)}><kbd>{item.rating}</kbd><strong>{item.label}</strong><small>{card.preview?.[item.key as keyof typeof card.preview] ? nextInterval(card.preview[item.key as keyof typeof card.preview]!) : "—"}</small></button>)}
          </div>}
        </div>
      ) : <section className="panel review-complete"><span><Check size={32} /></span><h2>На сегодня всё</h2><p>Все карточки повторены. Следующая очередь появится в рассчитанное FSRS время.</p><button className="secondary-button" onClick={() => void mutate(async () => undefined)}><RefreshCw size={16} /> Проверить снова</button></section>}
    </div>
  );
}

function AnalyticsView({ workspace }: ViewProps) {
  const series = normalizedSeries(workspace.analytics.focusSeries, 14);
  const goal = workspace.goals[0];
  return (
    <div className="page">
      <PageHeader eyebrow="Обратная связь" title="Аналитика" description="Сравнивайте намерение с реальным временем и корректируйте темп." actions={<button className="secondary-button"><Download size={16} /> Отчёт</button>} />
      <section className="analytics-summary">
        <MetricCard icon={Clock3} label="Фокус за 14 дней" value={formatDuration(workspace.analytics.focusTotalSeconds)} change="+12%" tone="indigo" />
        <MetricCard icon={CheckCircle2} label="Готово сегодня" value={String(workspace.dashboard.completedToday)} change="в ритме" tone="green" />
        <MetricCard icon={Brain} label="Повторений за 30 дней" value={String(workspace.analytics.reviews30d)} change={`${workspace.analytics.retention || 90}% удержание`} tone="amber" />
        <MetricCard icon={Target} label="Прогноз цели" value={`${Math.round(goal?.forecast.forecastPercent || 0)}%`} change={goal?.forecast.isFeasible ? "успеете" : "нужна корректировка"} tone="coral" />
      </section>
      <div className="analytics-grid">
        <section className="panel analytics-main-chart">
          <PanelHeading title="Время в фокусе" subtitle="Последние 14 дней" />
          <div className="analytics-chart">
            <ResponsiveContainer width="100%" height="100%">
              <BarChart data={series} barCategoryGap="28%">
                <CartesianGrid vertical={false} stroke="var(--line)" strokeDasharray="3 4" />
                <XAxis dataKey="label" axisLine={false} tickLine={false} tick={{ fill: "var(--muted)", fontSize: 11 }} />
                <YAxis axisLine={false} tickLine={false} tick={{ fill: "var(--muted)", fontSize: 11 }} tickFormatter={(value) => `${value}ч`} />
                <ChartTooltip content={<FocusTooltip />} />
                <Bar dataKey="hours" radius={[6, 6, 2, 2]}>{series.map((_, index) => <Cell key={index} fill={index === series.length - 1 ? "#5b5bd6" : "#b8b8eb"} />)}</Bar>
              </BarChart>
            </ResponsiveContainer>
          </div>
        </section>
        <section className="panel balance-card">
          <PanelHeading title="Распределение недели" />
          <div className="donut" style={{ "--donut": "conic-gradient(#5b5bd6 0 48%, #2f9d74 48% 76%, #e8aa59 76% 91%, #e87869 91%)" } as React.CSSProperties}><span><strong>{formatDuration(workspace.analytics.focusTotalSeconds)}</strong><small>всего</small></span></div>
          <div className="legend"><span><i className="indigo" /> Развитие <b>48%</b></span><span><i className="green" /> Работа <b>28%</b></span><span><i className="amber" /> Учёба <b>15%</b></span><span><i className="coral" /> Личное <b>9%</b></span></div>
        </section>
      </div>
      {goal && <section className="panel goal-forecast-row">
        <div className="goal-forecast-title"><span style={{ background: goal.color }}><Target size={19} /></span><div><strong>{goal.title}</strong><small>{format(new Date(goal.periodStart), "d MMM", { locale: ru })} — {format(new Date(goal.periodEnd), "d MMM", { locale: ru })}</small></div></div>
        <div className="forecast-metric"><small>Отслежено</small><strong>{formatDuration(goal.forecast.trackedSeconds)}</strong></div>
        <div className="forecast-metric"><small>Запланировано</small><strong>{formatDuration(goal.forecast.plannedSeconds)}</strong></div>
        <div className="forecast-metric"><small>Нужно в день</small><strong>{formatDuration(goal.forecast.requiredDailySeconds)}</strong></div>
        <div className={`forecast-status ${goal.forecast.isFeasible ? "ok" : "warning"}`}>{goal.forecast.isFeasible ? <CheckCircle2 size={16} /> : <AlarmClock size={16} />}{goal.forecast.isFeasible ? "По плану" : `Дефицит ${formatDuration(goal.forecast.shortfallSeconds)}`}</div>
      </section>}
    </div>
  );
}

function SettingsView({ user, workspace, theme, setTheme, reload }: ViewProps) {
  const [ankiState, setAnkiState] = useState<"idle" | "checking" | "connected" | "error">("idle");
  const [ankiVersion, setAnkiVersion] = useState<number | null>(null);
  const [ankiDecks, setAnkiDecks] = useState<string[]>([]);
  const [selectedDeck, setSelectedDeck] = useState("");
  const [syncing, setSyncing] = useState(false);
  const [pushState, setPushState] = useState(Notification.permission);
  const checkAnki = async () => {
    setAnkiState("checking");
    try {
      const [version, decks] = await Promise.all([ankiInvoke<number>("version"), ankiInvoke<string[]>("deckNames")]);
      setAnkiVersion(version); setAnkiDecks(decks); setSelectedDeck(decks[0] || ""); setAnkiState("connected"); toast.success("AnkiConnect найден");
    } catch { setAnkiState("error"); toast.error("Запустите Anki Desktop и установите AnkiConnect"); }
  };
  const syncAnki = async () => {
    const appDeck = workspace.decks[0];
    if (!appDeck || !selectedDeck) return;
    setSyncing(true);
    try {
      const noteIDs = await ankiInvoke<number[]>("findNotes", { query: `deck:"${selectedDeck.replaceAll('"', '\\"')}"` });
      const notes = noteIDs.length ? await ankiInvoke<Array<{ noteId: number; modelName: string; tags: string[]; fields: Record<string, { value: string }> }>>("notesInfo", { notes: noteIDs }) : [];
      const normalized = await Promise.all(notes.map(async (note) => {
        const values = Object.values(note.fields);
        const front = note.fields.Front?.value ?? values[0]?.value ?? "";
        const back = note.fields.Back?.value ?? values[1]?.value ?? "";
        return { ankiNoteId: note.noteId, deck: selectedDeck, model: note.modelName, front, back, tags: note.tags, hash: await contentHash({ front, back, tags: note.tags }) };
      }));
      await api("/anki/import", { method: "POST", ...jsonBody({ deckId: appDeck.id, notes: normalized }) });
      const exportCards = await api<Array<{ id: string; front: string; back: string; tags: string[]; deck: string; ankiNoteId?: number | null }>>("/anki/export");
      for (const card of exportCards.filter((item) => !item.ankiNoteId)) {
        const noteId = await ankiInvoke<number>("addNote", { note: { deckName: selectedDeck, modelName: "Basic", fields: { Front: card.front, Back: card.back }, options: { allowDuplicate: false }, tags: [...card.tags, "focusflow"] } });
        const hash = await contentHash({ front: card.front, back: card.back, tags: card.tags });
        await api("/anki/link", { method: "POST", ...jsonBody({ cardId: card.id, ankiNoteId: noteId, deck: selectedDeck, model: "Basic", hash }) });
      }
      toast.success(`Синхронизация завершена: ${normalized.length} из Anki`);
      await reload();
    } catch (error) { toast.error(error instanceof Error ? error.message : "Ошибка синхронизации"); }
    finally { setSyncing(false); }
  };
  const enablePush = async () => {
    try {
      const permission = await Notification.requestPermission(); setPushState(permission);
      if (permission !== "granted") return;
      const meta = await api<{ vapidPublicKey: string }>("/meta");
      if (!meta.vapidPublicKey) {
        new Notification("FocusFlow", { body: "Уведомления разрешены. Для фоновой доставки добавьте VAPID-ключи на сервере." });
        toast.success("Локальные уведомления включены");
        return;
      }
      const registration = await navigator.serviceWorker.ready;
      const subscription = await registration.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: base64ToArray(meta.vapidPublicKey) });
      await api("/push/subscriptions", { method: "POST", ...jsonBody(subscription.toJSON()) });
      toast.success("Push-уведомления включены");
    } catch (error) { toast.error(error instanceof Error ? error.message : "Не удалось включить уведомления"); }
  };
  return (
    <div className="page settings-page">
      <PageHeader eyebrow="Персонализация" title="Настройки" description="Управляйте профилем, внешними связями и поведением FocusFlow." />
      <div className="settings-layout">
        <aside className="settings-nav">
          <button className="active"><Settings size={16} /> Основные</button><button><Bell size={16} /> Уведомления</button><button><Repeat2 size={16} /> Интеграции</button><button><Cloud size={16} /> Данные</button>
        </aside>
        <div className="settings-content">
          <section className="panel settings-section">
            <PanelHeading title="Профиль" />
            <div className="profile-row"><div className="avatar large">{initials(user.displayName)}</div><div><strong>{user.displayName}</strong><span>{user.email}</span><small>{user.timezone} · русский</small></div><button className="secondary-button small">Изменить</button></div>
          </section>
          <section className="panel settings-section">
            <PanelHeading title="Внешний вид" subtitle="Тема сохраняется на этом устройстве" />
            <div className="theme-options">
              <button className={theme === "light" ? "active" : ""} onClick={() => setTheme("light")}><span className="theme-preview light-preview"><i /><b /><em /></span><strong><Sun size={15} /> Светлая</strong></button>
              <button className={theme === "dark" ? "active" : ""} onClick={() => setTheme("dark")}><span className="theme-preview dark-preview"><i /><b /><em /></span><strong><Moon size={15} /> Тёмная</strong></button>
            </div>
          </section>
          <section className="panel settings-section">
            <div className="integration-heading"><span className="integration-logo anki-logo"><Repeat2 size={20} /></span><div><h3>AnkiConnect</h3><p>Двусторонняя синхронизация содержимого карточек</p></div><span className={`connection-badge ${ankiState}`}>{ankiState === "connected" ? "Подключено" : ankiState === "error" ? "Недоступно" : "Не проверено"}</span></div>
            <div className="integration-body">
              <p>Anki Desktop должен быть запущен на этом компьютере. История повторений и расписание остаются независимыми.</p>
              {ankiState === "connected" && <label className="field"><span>Колода Anki</span><select value={selectedDeck} onChange={(event) => setSelectedDeck(event.target.value)}>{ankiDecks.map((deck) => <option key={deck}>{deck}</option>)}</select></label>}
              <div className="button-row">
                <button className="secondary-button" onClick={() => void checkAnki()} disabled={ankiState === "checking"}>{ankiState === "checking" ? <Loader2 className="spin" size={16} /> : <Laptop size={16} />} Проверить соединение</button>
                {ankiState === "connected" && <button className="primary-button" onClick={() => void syncAnki()} disabled={syncing}>{syncing ? <Loader2 className="spin" size={16} /> : <RefreshCw size={16} />} Синхронизировать</button>}
              </div>
              {ankiVersion && <small className="technical-note">AnkiConnect API {ankiVersion} · {ankiDecks.length} колод</small>}
            </div>
          </section>
          <section className="panel settings-section setting-row">
            <span className="setting-icon"><Bell size={19} /></span><div><strong>Системные уведомления</strong><p>Напоминания о задачах, блоках и завершении Pomodoro.</p></div><button className="secondary-button small" onClick={() => void enablePush()}>{pushState === "granted" ? <><Check size={15} /> Включены</> : "Включить"}</button>
          </section>
          <section className="panel settings-section setting-row">
            <span className="setting-icon"><Smartphone size={19} /></span><div><strong>Устанавливаемое приложение</strong><p>Откройте меню браузера и выберите «Установить FocusFlow».</p></div><span className="connection-badge connected">PWA готово</span>
          </section>
        </div>
      </div>
    </div>
  );
}

function AuthScreen({ onAuthenticated }: { onAuthenticated: (user: User) => Promise<void> }) {
  const [mode, setMode] = useState<"login" | "register">("register");
  const [busy, setBusy] = useState(false);
  const [serviceStatus, setServiceStatus] = useState<"checking" | "online" | "offline">("checking");
  const [form, setForm] = useState({ name: "", email: "", password: "", timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || "Asia/Tashkent" });
  useEffect(() => {
    let active = true;
    api<{ features: string[] }>("/meta")
      .then(() => { if (active) setServiceStatus("online"); })
      .catch(() => { if (active) setServiceStatus("offline"); });
    return () => { active = false; };
  }, []);
  const submit = async (event: React.FormEvent) => {
    event.preventDefault(); setBusy(true);
    try {
      const payload = mode === "login" ? { email: form.email, password: form.password } : form;
      const user = await api<User>(`/auth/${mode}`, { method: "POST", ...jsonBody(payload) });
      await onAuthenticated(user);
    } catch (error) { toast.error(error instanceof Error ? error.message : "Не удалось войти"); }
    finally { setBusy(false); }
  };
  const demo = async () => {
    setBusy(true);
    try { await onAuthenticated(await api<User>("/auth/demo", { method: "POST" })); }
    catch (error) {
      toast.error(serviceStatus === "offline"
        ? "API и PostgreSQL не запущены. Запустите базу и команду pnpm dev:api."
        : error instanceof Error ? error.message : "Демо доступно только локально");
    }
    finally { setBusy(false); }
  };
  return (
    <div className="auth-screen">
      <section className="auth-story">
        <div className="auth-brand"><span className="brand-mark">F<span /></span><strong>FocusFlow</strong></div>
        <div className="auth-copy"><span className="eyebrow light">СИСТЕМА СПОКОЙНОЙ ПРОДУКТИВНОСТИ</span><h1>Планируйте время.<br />Сохраняйте знания.<br /><em>Двигайтесь вперёд.</em></h1><p>Задачи, календарь, фокус‑таймер, заметки и интервальные повторения — в одном личном пространстве.</p></div>
        <div className="auth-proof"><span><Check size={15} /> Адаптивная PWA</span><span><Check size={15} /> PostgreSQL</span><span><Check size={15} /> FSRS + Anki</span></div>
        <div className="auth-orbits"><i /><i /><i /><span><Target size={28} /></span></div>
      </section>
      <section className="auth-form-side">
        <form className="auth-card" onSubmit={submit}>
          <div><span className="eyebrow">{mode === "register" ? "НОВОЕ ПРОСТРАНСТВО" : "С ВОЗВРАЩЕНИЕМ"}</span><h2>{mode === "register" ? "Начните с ясного плана" : "Продолжим с главного"}</h2><p>{mode === "register" ? "Первые задачи и карточки уже будут ждать внутри." : "Ваши задачи, время и знания синхронизированы."}</p></div>
          {mode === "register" && <label className="field"><span>Ваше имя</span><input autoFocus placeholder="Фердаус" value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} /></label>}
          <label className="field"><span>Email</span><input type="email" autoFocus={mode === "login"} required placeholder="you@example.com" value={form.email} onChange={(event) => setForm({ ...form, email: event.target.value })} /></label>
          <label className="field"><span>Пароль</span><input type="password" minLength={8} required placeholder="Минимум 8 символов" value={form.password} onChange={(event) => setForm({ ...form, password: event.target.value })} /></label>
          <button className="primary-button auth-submit" disabled={busy}>{busy ? <Loader2 className="spin" size={18} /> : mode === "register" ? <Sparkles size={18} /> : <ArrowRight size={18} />}{mode === "register" ? "Создать пространство" : "Войти"}</button>
          <div className="auth-divider"><span>или</span></div>
          <button type="button" className="secondary-button demo-button" onClick={() => void demo()} disabled={busy}><Play size={16} /> Открыть демо</button>
          <p className="auth-switch">{mode === "register" ? "Уже есть аккаунт?" : "Ещё нет аккаунта?"} <button type="button" onClick={() => setMode(mode === "register" ? "login" : "register")}>{mode === "register" ? "Войти" : "Создать"}</button></p>
          <div className={`service-status ${serviceStatus}`} role="status">
            <span>{serviceStatus === "offline" ? <WifiOff size={16} /> : serviceStatus === "online" ? <CheckCircle2 size={16} /> : <Cloud className="pulse" size={16} />}</span>
            <div>
              <strong>{serviceStatus === "online" ? "API и PostgreSQL подключены" : serviceStatus === "offline" ? "Сервер пока не запущен" : "Проверяем подключение…"}</strong>
              <small>{serviceStatus === "online" ? "Демо откроется с готовыми задачами и календарём" : serviceStatus === "offline" ? "UI работает на порту 5173, API ожидается на 8080" : "Это займёт пару секунд"}</small>
            </div>
          </div>
          <div className="auth-capabilities" aria-label="Возможности FocusFlow">
            <span><CalendarDays size={16} /><b>План</b><small>Календарь и цели</small></span>
            <span><TimerReset size={16} /><b>Фокус</b><small>Таймер и учёт</small></span>
            <span><FileText size={16} /><b>Заметки</b><small>BlockNote</small></span>
            <span><Brain size={16} /><b>Знания</b><small>FSRS и Anki</small></span>
          </div>
        </form>
      </section>
    </div>
  );
}

function TimerCard({ workspace, mutate, onOpen }: { workspace: Workspace; mutate: ViewProps["mutate"]; onOpen: () => void }) {
  const elapsed = useLiveElapsed(workspace.timer);
  const task = workspace.tasks.find((item) => item.id === workspace.timer.taskId) ?? workspace.tasks.find((item) => item.status !== "done");
  const running = workspace.timer.status === "running";
  const start = () => task && mutate(() => api("/timer/start", { method: "POST", ...jsonBody({ taskId: task.id, goalId: task.goalId, projectId: task.projectId, mode: "stopwatch" }) }));
  return (
    <section className={`timer-card ${running ? "running" : ""}`}>
      <div className="timer-card-top"><span><Zap size={15} fill="currentColor" /> {running ? "СЕЙЧАС В ФОКУСЕ" : "ФОКУС-ТАЙМЕР"}</span><button onClick={onOpen}><MoreHorizontal size={18} /></button></div>
      <strong className="timer-digits">{clock(elapsed)}</strong>
      <p>{workspace.timer.taskTitle || task?.title || "Выберите задачу"}</p>
      <div className="timer-card-actions">
        {workspace.timer.status === "idle" && <button className="timer-main-action" onClick={() => void start()}><Play size={17} fill="currentColor" /> Начать</button>}
        {workspace.timer.status === "running" && <><button className="timer-main-action" onClick={() => void mutate(() => api("/timer/pause", { method: "POST" }))}><Pause size={17} fill="currentColor" /> Пауза</button><button className="timer-stop" onClick={() => void mutate(() => api("/timer/stop", { method: "POST" }), "Сессия сохранена")}><Square size={15} fill="currentColor" /></button></>}
        {workspace.timer.status === "paused" && <><button className="timer-main-action" onClick={() => void mutate(() => api("/timer/resume", { method: "POST" }))}><Play size={17} fill="currentColor" /> Продолжить</button><button className="timer-stop" onClick={() => void mutate(() => api("/timer/stop", { method: "POST" }), "Сессия сохранена")}><Square size={15} fill="currentColor" /></button></>}
      </div>
    </section>
  );
}

function GoalMiniCard({ goal, onOpen, onPlan }: { goal: Goal; onOpen: () => void; onPlan: () => Promise<void> }) {
  const f = goal.forecast;
  return (
    <section className="panel goal-mini-card">
      <div className="goal-mini-head"><span style={{ background: goal.color }}><Target size={18} /></span><div><small>ЦЕЛЬ МЕСЯЦА</small><strong>{goal.title}</strong></div><button className="icon-button" onClick={onOpen}><ArrowRight size={16} /></button></div>
      <div className="goal-progress-copy"><strong>{Math.round(f.forecastPercent)}%</strong><span>{formatDuration(f.trackedSeconds + f.plannedSeconds)} из {formatDuration(goal.targetSeconds)}</span></div>
      <Progress.Root className="progress-root" value={Math.min(f.forecastPercent, 100)}><Progress.Indicator className="progress-indicator" style={{ transform: `translateX(-${100 - Math.min(f.forecastPercent, 100)}%)`, background: goal.color }} /></Progress.Root>
      <div className="goal-daily"><Clock3 size={16} /><span>Нужно <strong>{formatDuration(f.requiredDailySeconds)}</strong> в день</span><button onClick={() => void onPlan()}><Sparkles size={14} /> Спланировать</button></div>
    </section>
  );
}

function GoalCard({ goal, onPlan }: { goal: Goal; onPlan: () => Promise<void> }) {
  const f = goal.forecast;
  return (
    <section className="panel goal-card">
      <div className="goal-card-head"><span className="goal-icon" style={{ background: goal.color }}><Target size={20} /></span><button className="icon-button"><MoreHorizontal size={18} /></button></div>
      <div><span className="goal-period">{format(new Date(goal.periodStart), "MMMM", { locale: ru })}</span><h3>{goal.title}</h3><p>{formatDuration(goal.targetSeconds)} за период</p></div>
      <div className="goal-ring-row">
        <div className="small-ring" style={{ "--ring-progress": `${Math.min(f.forecastPercent, 100) * 3.6}deg`, "--ring-color": goal.color } as React.CSSProperties}><span><strong>{Math.round(f.forecastPercent)}%</strong><small>прогноз</small></span></div>
        <div className="goal-facts"><span><small>Отслежено</small><strong>{formatDuration(f.trackedSeconds)}</strong></span><span><small>Запланировано</small><strong>{formatDuration(f.plannedSeconds)}</strong></span><span><small>Осталось</small><strong>{formatDuration(f.remainingSeconds)}</strong></span></div>
      </div>
      <div className={`feasibility ${f.isFeasible ? "ok" : "warning"}`}>{f.isFeasible ? <CheckCircle2 size={16} /> : <AlarmClock size={16} />}<span>{f.isFeasible ? `Темп: ${formatDuration(f.requiredDailySeconds)} в день` : `Не хватает ${formatDuration(f.shortfallSeconds)} доступного времени`}</span></div>
      <button className="secondary-button goal-plan-button" onClick={() => void onPlan()}><Sparkles size={16} /> Пересобрать план</button>
    </section>
  );
}

function TaskRow({ task, compact = false, onToggle, onStart, onArchive }: { task: Task; compact?: boolean; onToggle: () => Promise<void>; onStart?: () => Promise<void>; onArchive?: () => Promise<void> }) {
  return (
    <div className={`task-row ${compact ? "compact" : ""} ${task.status === "done" ? "done" : ""}`}>
      <button className={`task-check priority-${task.priority}`} onClick={() => void onToggle()} aria-label={task.status === "done" ? "Вернуть задачу" : "Завершить задачу"}>{task.status === "done" ? <Check size={14} /> : <Circle size={14} />}</button>
      <div className="task-main"><strong>{task.title}</strong><div className="task-meta">{task.projectName && <span><i style={{ background: task.projectColor || "#5b5bd6" }} />{task.projectName}</span>}{task.dueAt && <span className={new Date(task.dueAt) < new Date() && task.status !== "done" ? "overdue" : ""}><CalendarDays size={13} />{smartDate(task.dueAt)}</span>}{task.estimateSeconds > 0 && <span><Clock3 size={13} />{formatDuration(task.estimateSeconds)}</span>}{task.recurrenceRule && <span><Repeat2 size={13} />Повтор</span>}</div></div>
      {!compact && task.tags?.slice(0, 2).map((tag) => <span className="task-tag" key={tag}><Tag size={11} />{tag}</span>)}
      {onStart && task.status !== "done" && <button className="task-play" onClick={() => void onStart()} aria-label="Запустить таймер"><Play size={14} fill="currentColor" /></button>}
      {onArchive && <DropdownMenu.Root><DropdownMenu.Trigger asChild><button className="icon-button"><MoreHorizontal size={16} /></button></DropdownMenu.Trigger><DropdownMenu.Portal><DropdownMenu.Content className="dropdown" align="end"><DropdownMenu.Item onSelect={() => void onArchive()}><Archive size={14} /> Архивировать</DropdownMenu.Item></DropdownMenu.Content></DropdownMenu.Portal></DropdownMenu.Root>}
    </div>
  );
}

function QuickTaskDialog({ open, onOpenChange, goals, onCreate }: { open: boolean; onOpenChange: (open: boolean) => void; goals: Goal[]; onCreate: (value: unknown) => Promise<void> }) {
  const [form, setForm] = useState({ title: "", dueAt: "", estimateMinutes: 30, priority: 3, goalId: "", recurrenceRule: "" });
  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    await onCreate({ title: form.title, dueAt: form.dueAt ? new Date(form.dueAt).toISOString() : null, estimateSeconds: form.estimateMinutes * 60, priority: form.priority, goalId: form.goalId || null, recurrenceRule: form.recurrenceRule || null, tags: [] });
    setForm({ title: "", dueAt: "", estimateMinutes: 30, priority: 3, goalId: "", recurrenceRule: "" }); onOpenChange(false);
  };
  return <Modal open={open} onOpenChange={onOpenChange} title="Новая задача" description="Сначала сформулируйте результат. Остальное можно уточнить позже.">
    <form onSubmit={submit} className="dialog-form">
      <label className="field"><span>Что нужно сделать?</span><input autoFocus required value={form.title} onChange={(event) => setForm({ ...form, title: event.target.value })} placeholder="Например, подготовить прототип" /></label>
      <div className="form-grid two">
        <label className="field"><span>Когда</span><input type="datetime-local" value={form.dueAt} onChange={(event) => setForm({ ...form, dueAt: event.target.value })} /></label>
        <label className="field"><span>Оценка, минут</span><input type="number" min={5} step={5} value={form.estimateMinutes} onChange={(event) => setForm({ ...form, estimateMinutes: Number(event.target.value) })} /></label>
      </div>
      <div className="form-grid two">
        <label className="field"><span>Приоритет</span><select value={form.priority} onChange={(event) => setForm({ ...form, priority: Number(event.target.value) })}><option value={1}>P1 · Критично</option><option value={2}>P2 · Важно</option><option value={3}>P3 · Обычно</option><option value={4}>P4 · Когда-нибудь</option></select></label>
        <label className="field"><span>Цель</span><select value={form.goalId} onChange={(event) => setForm({ ...form, goalId: event.target.value })}><option value="">Без цели</option>{goals.map((goal) => <option key={goal.id} value={goal.id}>{goal.title}</option>)}</select></label>
      </div>
      <label className="field"><span>Повторение</span><select value={form.recurrenceRule} onChange={(event) => setForm({ ...form, recurrenceRule: event.target.value })}><option value="">Не повторять</option><option value="FREQ=DAILY">Каждый день</option><option value="FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR">По будням</option><option value="FREQ=WEEKLY">Каждую неделю</option><option value="FREQ=MONTHLY">Каждый месяц</option></select></label>
      <DialogActions onCancel={() => onOpenChange(false)} submit="Добавить задачу" />
    </form>
  </Modal>;
}

function GoalDialog({ open, onOpenChange, onCreate }: { open: boolean; onOpenChange: (open: boolean) => void; onCreate: (value: unknown) => Promise<void> }) {
  const now = new Date();
  const [form, setForm] = useState({ title: "", targetHours: 120, periodStart: format(startOfMonth(now), "yyyy-MM-dd"), periodEnd: format(endOfMonth(now), "yyyy-MM-dd"), maxDailyHours: 8, sessionMinutes: 50, workdays: [1,2,3,4,5,6,0] });
  const submit = async (event: React.FormEvent) => { event.preventDefault(); await onCreate(form); onOpenChange(false); };
  const toggleDay = (day: number) => setForm({ ...form, workdays: form.workdays.includes(day) ? form.workdays.filter((item) => item !== day) : [...form.workdays, day] });
  return <Modal open={open} onOpenChange={onOpenChange} title="Новая месячная цель" description="FocusFlow рассчитает нужный ежедневный темп и проверит доступную ёмкость.">
    <form onSubmit={submit} className="dialog-form">
      <label className="field"><span>Название цели</span><input autoFocus required value={form.title} onChange={(event) => setForm({ ...form, title: event.target.value })} placeholder="Например, освоить Go" /></label>
      <div className="form-grid three"><label className="field"><span>Всего часов</span><input type="number" min={1} step={0.5} value={form.targetHours} onChange={(event) => setForm({ ...form, targetHours: Number(event.target.value) })} /></label><label className="field"><span>Макс. в день</span><input type="number" min={1} max={24} value={form.maxDailyHours} onChange={(event) => setForm({ ...form, maxDailyHours: Number(event.target.value) })} /></label><label className="field"><span>Сессия</span><select value={form.sessionMinutes} onChange={(event) => setForm({ ...form, sessionMinutes: Number(event.target.value) })}><option value={25}>25 минут</option><option value={50}>50 минут</option><option value={90}>90 минут</option></select></label></div>
      <div className="form-grid two"><label className="field"><span>Начало</span><input type="date" value={form.periodStart} onChange={(event) => setForm({ ...form, periodStart: event.target.value })} /></label><label className="field"><span>Конец</span><input type="date" value={form.periodEnd} onChange={(event) => setForm({ ...form, periodEnd: event.target.value })} /></label></div>
      <div className="field"><span>Доступные дни</span><div className="weekday-picker">{["Вс","Пн","Вт","Ср","Чт","Пт","Сб"].map((label, day) => <button type="button" key={label} className={form.workdays.includes(day) ? "active" : ""} onClick={() => toggleDay(day)}>{label}</button>)}</div></div>
      <div className="goal-live-calc"><Sparkles size={17} /><span>При равномерном распределении: <strong>{formatDuration(form.targetHours * 3600 / Math.max(daysBetween(form.periodStart, form.periodEnd), 1))} в календарный день</strong></span></div>
      <DialogActions onCancel={() => onOpenChange(false)} submit="Создать цель" />
    </form>
  </Modal>;
}

function CalendarBlockDialog({ open, onOpenChange, start, end, goals, onCreate }: { open: boolean; onOpenChange: (open: boolean) => void; start?: Date; end?: Date; goals: Goal[]; onCreate: (value: unknown) => Promise<void> }) {
  const initialStart = start ?? new Date(Date.now() + 3600_000);
  const initialEnd = end ?? new Date(initialStart.getTime() + 50 * 60_000);
  const [title, setTitle] = useState("");
  const [goalId, setGoalId] = useState("");
  const [startValue, setStartValue] = useState(toLocalInput(initialStart));
  const [endValue, setEndValue] = useState(toLocalInput(initialEnd));
  useEffect(() => { if (open) { setStartValue(toLocalInput(start ?? initialStart)); setEndValue(toLocalInput(end ?? initialEnd)); } }, [open, start, end]);
  const submit = async (event: React.FormEvent) => { event.preventDefault(); await onCreate({ title, goalId: goalId || null, startsAt: new Date(startValue).toISOString(), endsAt: new Date(endValue).toISOString(), locked: true }); setTitle(""); onOpenChange(false); };
  return <Modal open={open} onOpenChange={onOpenChange} title="Блок времени" description="Закрепите конкретный интервал под важную работу.">
    <form className="dialog-form" onSubmit={submit}><label className="field"><span>Название</span><input autoFocus required value={title} onChange={(event) => setTitle(event.target.value)} placeholder="Фокус-сессия" /></label><div className="form-grid two"><label className="field"><span>Начало</span><input type="datetime-local" value={startValue} onChange={(event) => setStartValue(event.target.value)} /></label><label className="field"><span>Конец</span><input type="datetime-local" value={endValue} onChange={(event) => setEndValue(event.target.value)} /></label></div><label className="field"><span>Связать с целью</span><select value={goalId} onChange={(event) => setGoalId(event.target.value)}><option value="">Без цели</option>{goals.map((goal) => <option key={goal.id} value={goal.id}>{goal.title}</option>)}</select></label><DialogActions onCancel={() => onOpenChange(false)} submit="Добавить в календарь" /></form>
  </Modal>;
}

function CommandPalette({ open, onOpenChange, workspace, navigate, onQuickTask }: { open: boolean; onOpenChange: (open: boolean) => void; workspace: Workspace; navigate: (view: View) => void; onQuickTask: () => void }) {
  const [query, setQuery] = useState("");
  const results = workspace.tasks.filter((task) => task.title.toLowerCase().includes(query.toLowerCase())).slice(0, 5);
  return <Modal open={open} onOpenChange={onOpenChange} className="command-dialog" title="">
    <div className="command-search"><Search size={19} /><input autoFocus value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Перейти, найти или создать…" /><kbd>Esc</kbd></div>
    <div className="command-results">
      <div className="command-caption">Быстрые действия</div>
      <button onClick={onQuickTask}><span><Plus size={17} /></span>Создать задачу<kbd>Q</kbd></button>
      <button onClick={() => navigate("focus")}><span><Play size={17} /></span>Открыть фокус</button>
      <button onClick={() => navigate("notes")}><span><FilePlus2 size={17} /></span>Новая заметка</button>
      {query && <><div className="command-caption">Задачи</div>{results.map((task) => <button key={task.id} onClick={() => navigate("tasks")}><span><Circle size={16} /></span>{task.title}<small>{task.projectName}</small></button>)}</>}
    </div>
  </Modal>;
}

function Modal({ open, onOpenChange, title, description, children, className = "" }: { open: boolean; onOpenChange: (open: boolean) => void; title: string; description?: string; children: React.ReactNode; className?: string }) {
  return <Dialog.Root open={open} onOpenChange={onOpenChange}><Dialog.Portal><Dialog.Overlay className="dialog-overlay" /><Dialog.Content className={`dialog-content ${className}`}><div className="dialog-heading">{title && <Dialog.Title>{title}</Dialog.Title>}{description && <Dialog.Description>{description}</Dialog.Description>}<Dialog.Close className="icon-button"><X size={18} /></Dialog.Close></div>{children}</Dialog.Content></Dialog.Portal></Dialog.Root>;
}

function DialogActions({ onCancel, submit }: { onCancel: () => void; submit: string }) {
  return <div className="dialog-actions"><button type="button" className="secondary-button" onClick={onCancel}>Отмена</button><button className="primary-button" type="submit">{submit}</button></div>;
}

function PageHeader({ eyebrow, title, description, actions }: { eyebrow: string; title: string; description: string; actions?: React.ReactNode }) {
  return <header className="page-header"><div><span className="eyebrow">{eyebrow}</span><h1>{title}</h1><p>{description}</p></div>{actions && <div className="page-actions">{actions}</div>}</header>;
}

function PanelHeading({ title, subtitle, action }: { title: string; subtitle?: string; action?: React.ReactNode }) {
  return <div className="panel-heading"><div><h3>{title}</h3>{subtitle && <span>{subtitle}</span>}</div>{action}</div>;
}

function EmptyState({ icon: Icon, title, text, action, compact = false }: { icon: typeof Circle; title: string; text: string; action?: React.ReactNode; compact?: boolean }) {
  return <div className={`empty-state ${compact ? "compact" : ""}`}><span><Icon size={compact ? 20 : 27} /></span><div><strong>{title}</strong><p>{text}</p></div>{action}</div>;
}

function MetricCard({ icon: Icon, label, value, change, tone }: { icon: typeof Clock3; label: string; value: string; change: string; tone: string }) {
  return <section className="panel metric-card"><span className={`metric-icon ${tone}`}><Icon size={19} /></span><small>{label}</small><strong>{value}</strong><em>{change}</em></section>;
}

function FocusTooltip({ active, payload, label }: { active?: boolean; payload?: Array<{ value: number }>; label?: string }) {
  if (!active || !payload?.length) return null;
  return <div className="chart-tooltip"><small>{label}</small><strong>{payload[0].value.toFixed(1)} ч</strong></div>;
}

function LoadingScreen() {
  return <div className="loading-screen"><span className="brand-mark large">F<span /></span><div className="loading-line"><i /></div><p>Собираем ваш день…</p></div>;
}
function ErrorScreen({ onRetry }: { onRetry: () => void }) {
  return <div className="error-screen"><span><WifiOff size={30} /></span><h1>Не удалось загрузить пространство</h1><p>Проверьте соединение с сервером и PostgreSQL.</p><button className="primary-button" onClick={onRetry}><RefreshCw size={16} /> Повторить</button></div>;
}

function useLiveElapsed(timer: TimerState) {
  const [elapsed, setElapsed] = useState(timer.elapsedSeconds ?? 0);
  useEffect(() => {
    setElapsed(timer.elapsedSeconds ?? 0);
    if (timer.status !== "running") return;
    const started = Date.now();
    const base = timer.elapsedSeconds ?? 0;
    const id = window.setInterval(() => setElapsed(base + Math.floor((Date.now() - started) / 1000)), 1000);
    return () => clearInterval(id);
  }, [timer.status, timer.startedAt, timer.elapsedSeconds]);
  return elapsed;
}

function normalizedSeries(input: Analytics["focusSeries"], days: number) {
  const map = new Map(input.map((item) => [item.date, item.seconds]));
  return Array.from({ length: days }, (_, index) => {
    const date = addDays(new Date(), index - days + 1);
    const key = format(date, "yyyy-MM-dd");
    return { date: key, label: format(date, days > 7 ? "d MMM" : "EE", { locale: ru }).replace(".", ""), hours: Math.round((map.get(key) || 0) / 360) / 10 };
  });
}

export function formatDuration(seconds: number) {
  if (!Number.isFinite(seconds) || seconds <= 0) return "0 мин";
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.round((seconds % 3600) / 60);
  if (!hours) return `${minutes} мин`;
  if (!minutes) return `${hours} ч`;
  return `${hours} ч ${minutes} мин`;
}
export function clock(seconds: number) { const safe = Math.max(Math.floor(seconds || 0), 0); return `${String(Math.floor(safe / 60)).padStart(2, "0")}:${String(safe % 60).padStart(2, "0")}`; }
function initials(name: string) { return name.split(/\s+/).slice(0,2).map((part) => part[0]).join("").toUpperCase(); }
function firstName(name: string) { return name.split(/\s+/)[0] || name; }
function smartDate(value: string) { const date = new Date(value); if (isSameDay(date, new Date())) return `Сегодня, ${format(date, "HH:mm")}`; if (isSameDay(date, addDays(new Date(), 1))) return `Завтра, ${format(date, "HH:mm")}`; return format(date, "d MMM, HH:mm", { locale: ru }); }
function relativeDate(value: string) { const date = new Date(value); const diff = date.getTime() - Date.now(); if (Math.abs(diff) < 3600_000) return `${Math.max(Math.round(Math.abs(diff) / 60_000), 1)} мин ${diff < 0 ? "назад" : "вперёд"}`; return smartDate(value); }
function nextInterval(value: string) { const diff = +new Date(value) - Date.now(); if (diff < 3600_000) return `${Math.max(Math.round(diff / 60_000), 1)} мин`; if (diff < 86_400_000) return `${Math.round(diff / 3600_000)} ч`; return `${Math.round(diff / 86_400_000)} д`; }
function plainCard(value: string) { return value.replace(/<[^>]*>/g, "").replace(/\{\{c\d+::(.*?)(?:::.*?)?\}\}/g, "$1"); }
function toLocalInput(date: Date) { const offset = date.getTimezoneOffset(); return new Date(date.getTime() - offset * 60_000).toISOString().slice(0,16); }
export function daysBetween(start: string, end: string) { return Math.floor((+new Date(end) - +new Date(start)) / 86_400_000) + 1; }
function base64ToArray(value: string) { const padding = "=".repeat((4 - value.length % 4) % 4); const raw = atob((value + padding).replace(/-/g, "+").replace(/_/g, "/")); return Uint8Array.from([...raw].map((char) => char.charCodeAt(0))); }
