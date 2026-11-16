import "./app.scss";
import "./ui/system.scss";
import type { PropsWithChildren } from "react";
import { MobileThemeProvider } from "./theme";

export default function App(props: PropsWithChildren) {
  return <MobileThemeProvider>{props.children}</MobileThemeProvider>;
}
