import { useEffect, useState } from 'react';
import { AdminLayout } from '../shared/layouts/AdminLayout';
import { DashboardPage } from './pages/DashboardPage';
import { PromptStrategyPage } from './pages/PromptStrategyPage';
import { PresetLibraryPage } from './pages/PresetLibraryPage';
import { ShuihuoModelCatalogPage } from './pages/ShuihuoModelCatalogPage';
import { AgentSkillLibraryPage } from './pages/AgentSkillLibraryPage';
import { ErrorLogPage } from './pages/ErrorLogPage';
import { Link } from '../shared/components/Link';

function usePathname() {
  const [pathname, setPathname] = useState(window.location.pathname);

  useEffect(() => {
    const handlePopState = () => setPathname(window.location.pathname);
    window.addEventListener('popstate', handlePopState);
    return () => window.removeEventListener('popstate', handlePopState);
  }, []);

  return pathname;
}

function getPage(pathname) {
  if (pathname === '/admin/presets') return <PresetLibraryPage />;
  if (pathname === '/admin/agent-skills') return <AgentSkillLibraryPage />;
  if (pathname === '/admin/prompts') return <PromptStrategyPage />;
  if (pathname === '/admin/shuihuo-models') return <ShuihuoModelCatalogPage />;
  if (pathname === '/admin/error-logs') return <ErrorLogPage />;
  return <DashboardPage />;
}

export function AdminApp() {
  const pathname = usePathname();
  useEffect(() => {
    if (pathname === '/admin/accounts') window.location.replace('/accounts');
  }, [pathname]);
  if (pathname === '/admin/accounts') return <Link href="/accounts" reload>前往账号与角色</Link>;
  return <AdminLayout>{getPage(pathname)}</AdminLayout>;
}
