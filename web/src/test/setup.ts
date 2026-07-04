import "@testing-library/jest-dom";
import { webcrypto } from "node:crypto";

// jsdom's window.crypto has getRandomValues but no subtle.digest, which oauth.ts needs for the
// PKCE S256 challenge. Swap in Node's own WebCrypto implementation for the whole test run.
Object.defineProperty(globalThis, "crypto", {
  value: webcrypto,
  configurable: true,
});
