import "@testing-library/jest-dom";
import { webcrypto } from "node:crypto";
import { vi } from "vitest";

// MDXEditor's style.css import breaks under vitest/jsdom the same way react-diff-view's does.
// Stub it globally so any test that transitively imports MarkdownEditor doesn't choke.
vi.mock("@mdxeditor/editor/style.css", () => ({}));

// jsdom's window.crypto has getRandomValues but no subtle.digest, which oauth.ts needs for the
// PKCE S256 challenge. Swap in Node's own WebCrypto implementation for the whole test run.
Object.defineProperty(globalThis, "crypto", {
  value: webcrypto,
  configurable: true,
});
