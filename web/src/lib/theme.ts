export type Theme = "light" | "dark";

const listeners = new Set<() => void>();
let current: Theme = "light";

export function getInitialTheme(): Theme {
  const stored = localStorage.getItem("theme");
  if (stored === "dark" || stored === "light") return stored;
  if (typeof window !== "undefined" && window.matchMedia?.("(prefers-color-scheme: dark)").matches) {
    return "dark";
  }
  return "light";
}

export function applyTheme(theme: Theme): void {
  current = theme;
  const root = document.documentElement;
  root.classList.toggle("dark", theme === "dark");
  localStorage.setItem("theme", theme);
  listeners.forEach((cb) => cb());
}

export function getTheme(): Theme {
  return current;
}

export function toggleTheme(): void {
  applyTheme(current === "dark" ? "light" : "dark");
}

export function subscribe(cb: () => void): () => void {
  listeners.add(cb);
  return () => listeners.delete(cb);
}
