declare const __MINIPROGRAM_API_ORIGIN__: string;

const configuredOrigin = String(
  typeof __MINIPROGRAM_API_ORIGIN__ === "string" ? __MINIPROGRAM_API_ORIGIN__ : "",
).trim();

// The placeholder deliberately cannot resolve. A real HTTPS domain is supplied
// through the private developer configuration before preview or release.
export const PLACEHOLDER_API_ORIGIN = "https://api.example.invalid";
export const API_ORIGIN_CONFIGURATION_ERROR = "服务端地址未配置，请使用已配置微信合法域名的 HTTPS 构建";
export const API_ORIGIN = configuredOrigin || PLACEHOLDER_API_ORIGIN;

export function isApiOriginConfigured() {
  return API_ORIGIN !== PLACEHOLDER_API_ORIGIN;
}

export function requireApiOrigin() {
  if (!isApiOriginConfigured()) {
    throw new Error(API_ORIGIN_CONFIGURATION_ERROR);
  }
}

export function apiUrl(path: string) {
  return new URL(path, API_ORIGIN).toString();
}
