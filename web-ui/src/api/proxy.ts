import api from './index';
import type { ProxyStatus, ProxyStartRequest, ProxyStopRequest, ProxyConfigRequest } from '../types/proxy';

export const getProxyStatus = () => api.get<ProxyStatus>('/status');

export const startProxy = (data: ProxyStartRequest) =>
  api.post('/proxy/start', data);

export const stopProxy = (data: ProxyStopRequest) =>
  api.post('/proxy/stop', data);

export const saveProxyConfig = (data: ProxyConfigRequest) =>
  api.post('/proxy/config', data);
