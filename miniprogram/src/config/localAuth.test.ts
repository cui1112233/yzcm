import { expect, test } from "vitest";
import { isLoopbackOrigin } from "./localAuth";

test.each(["http://127.0.0.1:13192", "http://localhost:13192", "http://[::1]:13192"])("permits local account login for %s", (origin) => {
  expect(isLoopbackOrigin(origin)).toBe(true);
});
test.each(["https://yzcm.site", "http://localhost.evil.test", "http://127.0.0.1.evil.test", "https://127.0.0.1@evil.test", "invalid", "file://localhost/"])("rejects non-local credential target %s", (origin) => {
  expect(isLoopbackOrigin(origin)).toBe(false);
});
