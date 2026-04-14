import api from './index';
import type { UnifiedConfig, TimeoutConfig, LimiterConfig, SecurityConfig } from '../types/api';

export const getConfig = () => api.get<UnifiedConfig>('/config');

export const saveConfig = (data: {
  timeout?: TimeoutConfig;
  limiter?: LimiterConfig;
  system?: { autostartEnabled: boolean };
  security?: SecurityConfig;
}) => api.post('/config', data);
