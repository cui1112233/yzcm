// Config is authoritative for permissions. Member details and remote quota
// probes may finish later without withholding a successful model catalog.
export async function refreshApiConfigModels(api, view) {
  view.quotaLoading(true);
  void Promise.resolve().then(api.getQuotas).then(view.quotas).catch(view.quotaError).finally(() => view.quotaLoading(false));
  view.models(await Promise.resolve().then(api.listModels));
}

export async function loadApiConfigPage(api, view) {
  const read = action => Promise.resolve().then(action);
  void read(api.getMemberCenter).then(view.center).catch(error => view.error('member', error));
  let stage = 'config';
  try {
    const config = await read(api.getConfig);
    view.config(config);
    if (api.canManage(config)) {
      stage = 'models';
      await refreshApiConfigModels(api, view);
    }
  } catch (error) { view.error(stage, error); }
  finally { view.ready(); }
}
