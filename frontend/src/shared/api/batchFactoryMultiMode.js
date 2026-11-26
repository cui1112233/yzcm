import {apiRequest,LONG_RUNNING_API_TIMEOUT_MS} from './client.js';
export const createMultiModeBatches=payload=>apiRequest('/api/batch-factory/v12/multi-mode-batches',{method:'POST',body:JSON.stringify(payload),timeoutMs:LONG_RUNNING_API_TIMEOUT_MS});
export const resumeMultiModeSources=groupId=>apiRequest(`/api/batch-factory/v12/multi-mode-batches/${encodeURIComponent(groupId)}/resume`,{method:'POST',body:'{}',timeoutMs:LONG_RUNNING_API_TIMEOUT_MS});
