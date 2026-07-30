self.addEventListener("push", (event) => {
  let payload = { title: "FocusFlow", body: "Пора вернуться к важному", url: "/" };
  try {
    payload = { ...payload, ...(event.data ? event.data.json() : {}) };
  } catch {
    payload.body = event.data?.text() || payload.body;
  }
  event.waitUntil(self.registration.showNotification(payload.title, {
    body: payload.body,
    icon: "/favicon.svg",
    badge: "/favicon.svg",
    tag: payload.url,
    renotify: true,
    data: { url: payload.url },
    actions: [
      { action: "open", title: "Открыть" },
      { action: "dismiss", title: "Позже" }
    ]
  }));
});

self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  if (event.action === "dismiss") return;
  const target = event.notification.data?.url || "/";
  event.waitUntil(clients.matchAll({ type: "window", includeUncontrolled: true }).then((windows) => {
    const existing = windows.find((client) => "focus" in client);
    if (existing) {
      existing.navigate(target);
      return existing.focus();
    }
    return clients.openWindow(target);
  }));
});
