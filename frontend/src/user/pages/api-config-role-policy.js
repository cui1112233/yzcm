const NONE_POLICY = Object.freeze({
  mode: 'none',
  canConfigureAPI: false,
  canAddCustomModel: false,
  canEditAddress: false,
  catalogSource: 'none'
});

export function projectApiConfigCapabilities(config) {
  if (!config || config.canConfigureAPI !== true) return { ...NONE_POLICY };
  const full = config.canAddCustomModel === true && config.canEditAddress === true && config.catalogSource === 'full';
  if (full) {
    return {
      mode: 'full',
      canConfigureAPI: true,
      canAddCustomModel: true,
      canEditAddress: true,
      catalogSource: 'full'
    };
  }
  const restricted = config.canAddCustomModel === false && config.canEditAddress === false && config.catalogSource === 'yzcm_saved';
  if (restricted) {
    return {
      mode: 'restricted',
      canConfigureAPI: true,
      canAddCustomModel: false,
      canEditAddress: false,
      catalogSource: 'yzcm_saved'
    };
  }
  return { ...NONE_POLICY };
}
