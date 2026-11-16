import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";

type ResolveOptions = {
  root?: string;
  mode?: string;
  environment?: NodeJS.ProcessEnv;
};

const FALLBACK_ORIGIN = "https://api.example.invalid";

function readEnvFile(path: string) {
  if (!existsSync(path)) return {};
  return Object.fromEntries(
    readFileSync(path, "utf8")
      .split(/\r?\n/)
      .map((line) => line.trim())
      .filter((line) => line && !line.startsWith("#"))
      .map((line) => {
        const separator = line.indexOf("=");
        if (separator < 0) return ["", ""];
        return [line.slice(0, separator).trim(), line.slice(separator + 1).trim()];
      })
      .filter(([key]) => key),
  ) as Record<string, string>;
}

export function resolveApiOrigin({
  root = process.cwd(),
  mode = process.env.NODE_ENV === "production" ? "production" : "development",
  environment = process.env,
}: ResolveOptions = {}) {
  const loaded = [
    ".env",
    ".env.local",
    `.env.${mode}`,
    `.env.${mode}.local`,
  ].reduce<Record<string, string>>((values, filename) => {
    return { ...values, ...readEnvFile(join(root, filename)) };
  }, {});

  return String(environment.TARO_APP_API_ORIGIN || loaded.TARO_APP_API_ORIGIN || FALLBACK_ORIGIN).trim();
}
