import workspaceLight from "../assets/icons/tab-workspace-normal-light.png";
import workspaceSelectedLight from "../assets/icons/tab-workspace-selected-light.png";
import tasksLight from "../assets/icons/tab-tasks-normal-light.png";
import tasksSelectedLight from "../assets/icons/tab-tasks-selected-light.png";
import functionsLight from "../assets/icons/tab-functions-normal-light.png";
import functionsSelectedLight from "../assets/icons/tab-functions-selected-light.png";
import profileLight from "../assets/icons/tab-profile-normal-light.png";
import profileSelectedLight from "../assets/icons/tab-profile-selected-light.png";
import workspaceDark from "../assets/icons/tab-workspace-normal-dark.png";
import workspaceSelectedDark from "../assets/icons/tab-workspace-selected-dark.png";
import tasksDark from "../assets/icons/tab-tasks-normal-dark.png";
import tasksSelectedDark from "../assets/icons/tab-tasks-selected-dark.png";
import functionsDark from "../assets/icons/tab-functions-normal-dark.png";
import functionsSelectedDark from "../assets/icons/tab-functions-selected-dark.png";
import profileDark from "../assets/icons/tab-profile-normal-dark.png";
import profileSelectedDark from "../assets/icons/tab-profile-selected-dark.png";

export const nativeTabIcons = {
  light: [[workspaceLight, workspaceSelectedLight], [tasksLight, tasksSelectedLight], [functionsLight, functionsSelectedLight], [profileLight, profileSelectedLight]],
  dark: [[workspaceDark, workspaceSelectedDark], [tasksDark, tasksSelectedDark], [functionsDark, functionsSelectedDark], [profileDark, profileSelectedDark]],
} as const;
