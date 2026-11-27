import React from 'react';
import { createRoot } from 'react-dom/client';
import 'antd/dist/reset.css';
import '../shared/styles/global.css';
// V88 设计令牌：只新增 --v88-* 变量与按钮/空态基类，后续页面按批次接入。
import '../shared/styles/v88-tokens.css';
// 风格层：html[data-scheme] × html[data-theme] 四套 × 深浅两档，只覆盖颜色变量。
import '../shared/styles/theme-schemes.css';
import '../shared/styles/v88-ui.css';
import '../shared/styles/agent-workspace.css';
import '../shared/styles/agent-chat.css';
import '../shared/styles/member-center.css';
import '../shared/styles/team-governance.css';
import '../shared/styles/team-collaboration.css';
import '../shared/styles/login-card.css';
// Single visual authority for the nine approved personal-center screens.
import '../shared/styles/account-center-pixel-parity.css';
import '../shared/styles/cinematic-glass.css';
import '../shared/styles/frosted-surfaces.css';
import { installClientErrorReporting } from '../shared/error-reporting';
import { UserApp } from './App';
import { installBatchCompletionNotifications } from '../shared/notifications/installBatchCompletionNotifications.js';

installClientErrorReporting();
installBatchCompletionNotifications();

createRoot(document.getElementById('root')).render(
  <React.StrictMode>
    <UserApp />
  </React.StrictMode>
);
