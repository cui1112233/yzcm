// Config is authoritative for permissions. Member details and remote quota
// probes may finish later without withholding a successful model catalog.
export async function loadApiConfigPage(api, view) {
  const read = action => Promise.resolve().then(action);
  void read(api.getMemberCenter).then(view.center).catch(error => view.error('member', error));
  let stage = 'config';
  try {
    const config = await read(api.getConfig);
    view.config(config);
    if (api.canManage(config)) {
      view.quotaLoading(true);
      void read(api.getQuotas).then(view.quotas).catch(view.quotaError).finally(() => view.quotaLoading(false));
      stage = 'models';
      view.models(await read(api.listModels));
    }
  } catch (error) { view.error(stage, error); }
  finally { view.ready(); }
}
