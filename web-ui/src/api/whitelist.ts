import api from './index';

export const getWhitelist = () => api.get<string[]>('/whitelist');

export const addWhitelistIP = (ip: string) =>
  api.post('/whitelist', { ip });

export const deleteWhitelistIP = (ip: string) =>
  api.delete('/whitelist', { data: { ip } });
