import api from './index';
import type { SystemSettings } from '../types/api';

export const getSystemSettings = () => api.get<SystemSettings>('/system/settings');

export const saveSystemSettings = (autostartEnabled: boolean) =>
  api.post('/system/settings', { autostartEnabled });

export const shutdownApplication = () => api.post('/shutdown');
