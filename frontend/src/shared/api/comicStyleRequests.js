const id = value => encodeURIComponent(String(value));

// Capture one immutable output selection for a media view and all its actions.
// The caller provides the authenticated client; no model calls happen on select.
export function createComicStyleMediaAPI(request, batchId, bookId, styleId = '') {
  const base = `/api/batch-factory/v12/batches/${id(batchId)}/books/${id(bookId)}`;
  const scoped = (path, values = {}) => {
    const pairs = Object.entries({ ...values, styleId }).filter(([, value]) => value !== '' && value != null);
    return `${path}${pairs.length ? `?${pairs.map(([key, value]) => `${id(key)}=${id(value)}`).join('&')}` : ''}`;
  };
  const get = (path, options = {}) => { const { view, ...rest } = options; return request(scoped(path, { view }), { ...rest, cache: 'no-store' }); };
  const send = (path, method, payload = {}, options = {}) => request(path, { ...options, method, body: JSON.stringify({ ...payload, styleId }), suppressGlobalError: true });
  const pages = `${base}/comic-pages`, collections = `${base}/comic-collections`;
  return {
    listPages: options => get(pages, options),
    listCollections: options => get(collections, options),
    generatePage: (videoId, payload, options) => send(`${pages}/${id(videoId)}/generate`, 'POST', payload, options),
    updatePage: (videoId, payload) => send(`${pages}/${id(videoId)}`, 'PUT', payload),
    promptPreview: (videoId, payload) => send(`${pages}/${id(videoId)}/prompt-preview`, 'POST', payload),
    uploadPage: (videoId, dataUrl) => send(`${pages}/${id(videoId)}/images`, 'POST', { dataUrl }),
    primaryImage: (pageId, imageId) => send(`${pages}/${id(pageId)}/images/${id(imageId)}/primary`, 'PUT'),
    references: (videoId, referenceImageUrls) => send(`${pages}/${id(videoId)}/reference-images`, 'PUT', { referenceImageUrls }),
    applyReferences: videoId => send(`${pages}/${id(videoId)}/reference-images/apply-to-all`, 'POST'),
    createCollection: payload => send(collections, 'POST', payload),
    generateBook: options => send(`${collections}/generate`, 'POST', {}, options),
    publish: (collectionId, payload) => send(`${collections}/${id(collectionId)}/publish`, 'POST', payload),
    publishRecord: (collectionId, options) => get(`${collections}/${id(collectionId)}/publish`, options),
    downloadPages: () => get(`${pages}/images-zip`, { responseType: 'blob' }),
    downloadPrompts: () => get(`${pages}/download-zip`, { responseType: 'blob' }),
    downloadDouyin: () => get(`${pages}/douyin-zip`, { responseType: 'blob' }),
    downloadCollection: collectionId => get(`${collections}/${id(collectionId)}/download`, { responseType: 'blob' })
  };
}
