import { Image } from "@tarojs/components";
import arrowLeft from "../assets/icons/arrow-left.png";
import bookOpen from "../assets/icons/book-open.png";
import bot from "../assets/icons/bot.png";
import camera from "../assets/icons/camera.png";
import chart from "../assets/icons/chart-no-axes-combined.png";
import chevronRight from "../assets/icons/chevron-right.png";
import circleDashed from "../assets/icons/circle-dashed.png";
import clapperboard from "../assets/icons/clapperboard.png";
import crown from "../assets/icons/crown.png";
import factory from "../assets/icons/factory.png";
import filePenLine from "../assets/icons/file-pen-line.png";
import folderClock from "../assets/icons/folder-clock.png";
import home from "../assets/icons/home.png";
import keyRound from "../assets/icons/key-round.png";
import notebookTabs from "../assets/icons/notebook-tabs.png";
import refreshCcw from "../assets/icons/refresh-ccw.png";
import settings from "../assets/icons/settings-2.png";
import shieldCheck from "../assets/icons/shield-check.png";
import sliders from "../assets/icons/sliders-horizontal.png";
import sparkles from "../assets/icons/sparkles.png";
import sun from "../assets/icons/sun.png";
import timer from "../assets/icons/timer.png";
import userRound from "../assets/icons/user-round.png";
import usersRound from "../assets/icons/users-round.png";
import volume from "../assets/icons/volume-2.png";
import search from "../assets/icons/search.png";
import plus from "../assets/icons/plus.png";

export const APP_ICON_NAMES = [
  "home", "folder-clock", "notebook-tabs", "user-round", "file-pen-line", "book-open",
  "clapperboard", "bot", "factory", "crown", "shield-check", "users-round", "key-round",
  "chart", "sun", "settings", "arrow-left", "chevron-right", "refresh-ccw", "camera",
  "sparkles", "timer", "volume", "sliders", "circle-dashed", "search", "plus",
] as const;

export type AppIconName = typeof APP_ICON_NAMES[number];

const ICONS: Record<AppIconName, string> = {
  home,
  "folder-clock": folderClock,
  "notebook-tabs": notebookTabs,
  "user-round": userRound,
  "file-pen-line": filePenLine,
  "book-open": bookOpen,
  clapperboard,
  bot,
  factory,
  crown,
  "shield-check": shieldCheck,
  "users-round": usersRound,
  "key-round": keyRound,
  chart,
  sun,
  settings,
  "arrow-left": arrowLeft,
  "chevron-right": chevronRight,
  "refresh-ccw": refreshCcw,
  camera,
  sparkles,
  timer,
  volume,
  sliders,
  "circle-dashed": circleDashed,
  search,
  plus,
};

type AppIconProps = {
  name: AppIconName;
  size?: number;
  className?: string;
};

export default function AppIcon({ name, size = 20, className = "" }: AppIconProps) {
  return <Image className={`mp-app-icon ${className}`.trim()} src={ICONS[name]} mode="aspectFit" style={{ width: `${size}px`, height: `${size}px` }} />;
}
