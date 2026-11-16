import Taro from "@tarojs/taro";
import { authStorageKey } from "../config/localAuth";

export type LoginPreferences = { remember: boolean; automatic: boolean; username: string };
const key = "yzcm.miniprogram.loginPreferences";
export function readLoginPreferences(): LoginPreferences {
  const saved = Taro.getStorageSync(authStorageKey(key));
  const remember = saved?.remember === true;
  return { remember, automatic: remember && saved?.automatic === true, username: remember && typeof saved?.username === "string" ? saved.username : "" };
}
export function saveLoginPreferences(value: LoginPreferences) {
  Taro.setStorageSync(authStorageKey(key), { remember: value.remember, automatic: value.remember && value.automatic, username: value.remember ? value.username.trim() : "" });
}
