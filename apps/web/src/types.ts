export type ID = string;

export interface User {
  id: ID;
  email: string;
  displayName: string;
  timezone: string;
  locale: string;
  theme: "light" | "dark" | "system";
  version: number;
}

export interface Dashboard {
  openTasks: number;
  completedToday: number;
  dueCards: number;
  notes: number;
  trackedTodaySeconds: number;
  greeting: string;
}

export interface Task {
  id: ID;
  title: string;
  status: "todo" | "doing" | "done" | "cancelled";
  priority: 1 | 2 | 3 | 4;
  tags: string[];
  dueAt: string | null;
  deadlineAt: string | null;
  estimateSeconds: number;
  projectId: ID | null;
  goalId: ID | null;
  parentId: ID | null;
  recurrenceRule: string | null;
  completedAt: string | null;
  version: number;
  projectName?: string | null;
  projectColor?: string | null;
}

export interface Forecast {
  targetSeconds: number;
  trackedSeconds: number;
  plannedSeconds: number;
  remainingSeconds: number;
  unscheduledSeconds: number;
  eligibleDays: number;
  requiredDailySeconds: number;
  availableSeconds: number;
  shortfallSeconds: number;
  completionPercent: number;
  forecastPercent: number;
  isFeasible: boolean;
}

export interface Goal {
  id: ID;
  title: string;
  targetSeconds: number;
  periodStart: string;
  periodEnd: string;
  maxDailySeconds: number;
  sessionSeconds: number;
  color: string;
  workdays: number[];
  preferredStart: string;
  preferredEnd: string;
  version: number;
  forecast: Forecast;
}

export interface CalendarBlock {
  id: ID;
  title: string;
  startsAt: string;
  endsAt: string;
  color: string;
  source: "manual" | "planner" | "ics";
  locked: boolean;
  completedAt: string | null;
  taskId: ID | null;
  goalId: ID | null;
  version: number;
}

export interface TimerState {
  taskId?: ID | null;
  goalId?: ID | null;
  projectId?: ID | null;
  calendarBlockId?: ID | null;
  taskTitle?: string | null;
  mode: "stopwatch" | "pomodoro";
  status: "idle" | "running" | "paused";
  startedAt?: string | null;
  accumulatedSeconds: number;
  elapsedSeconds: number;
  targetSeconds?: number | null;
  version: number;
}

export interface Note {
  id: ID;
  parentId: ID | null;
  title: string;
  content: unknown[];
  plainText: string;
  icon: string;
  position: number;
  updatedAt: string;
  version: number;
}

export interface Deck {
  id: ID;
  name: string;
  color: string;
  newPerDay: number;
  reviewsPerDay: number;
  desiredRetention: number;
  version: number;
  total: number;
  due: number;
}

export interface Card {
  id: ID;
  deckId: ID;
  deckName: string;
  cardType: "basic" | "cloze";
  front: string;
  back: string;
  tags: string[];
  dueAt: string;
  sourceNoteId: ID | null;
  version: number;
  preview?: Record<"again" | "hard" | "good" | "easy", string>;
}

export interface Analytics {
  focusSeries: Array<{ date: string; seconds: number }>;
  focusTotalSeconds: number;
  reviews30d: number;
  retention: number;
}

export interface NotificationItem {
  id: ID;
  title: string;
  remindAt: string;
  status: "scheduled" | "sent" | "snoozed";
  version: number;
}

