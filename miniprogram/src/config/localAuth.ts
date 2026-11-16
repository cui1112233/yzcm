import { apiUrl } from "./runtime";

export function isLoopbackOrigin(origin: string) {
  try {
    const url = new URL(origin);
    return ["http:", "https:"].includes(url.protocol)
      && ["127.0.0.1", "localhost", "[::1]"].includes(url.hostname)
      && !url.username && !url.password;
  } catch {
    return false;
  }
}

export function isLocalAccountLogin() {
  return isLoopbackOrigin(apiUrl("/"));
}

export function authStorageKey(key: string) {
  return `${key}:${new URL(apiUrl("/")).origin}`;
}
