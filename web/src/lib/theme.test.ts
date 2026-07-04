import { beforeEach, describe, expect, it } from "vitest";
import { applyTheme, getInitialTheme, getTheme, toggleTheme } from "./theme";

describe("theme store", () => {
  beforeEach(() => {
    localStorage.clear();
    document.documentElement.classList.remove("dark");
  });

  it("getInitialTheme honours a persisted choice", () => {
    localStorage.setItem("theme", "dark");
    expect(getInitialTheme()).toBe("dark");
  });

  it("applyTheme('dark') sets the .dark class and persists", () => {
    applyTheme("dark");
    expect(document.documentElement.classList.contains("dark")).toBe(true);
    expect(localStorage.getItem("theme")).toBe("dark");
    expect(getTheme()).toBe("dark");
  });

  it("applyTheme('light') removes the .dark class", () => {
    applyTheme("dark");
    applyTheme("light");
    expect(document.documentElement.classList.contains("dark")).toBe(false);
    expect(localStorage.getItem("theme")).toBe("light");
  });

  it("toggleTheme flips the current theme", () => {
    applyTheme("light");
    toggleTheme();
    expect(getTheme()).toBe("dark");
  });
});
