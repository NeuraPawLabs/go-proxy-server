import api from './index';
import type { TimeoutConfig } from '../types/api';

export const getTimeout = () => api.get<TimeoutConfig>('/timeout');

export const saveTimeout = (data: TimeoutConfig) =>
  api.post('/timeout', data);
