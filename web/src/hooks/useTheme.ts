import { useSyncExternalStore } from "react";
import { getTheme, subscribe, toggleTheme, type Theme } from "@/lib/theme";

export function useTheme(): { theme: Theme; toggle: () => void } {
  const theme = useSyncExternalStore(subscribe, getTheme, getTheme);
  return { theme, toggle: toggleTheme };
}
