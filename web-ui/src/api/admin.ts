import api from './index';
import type { AdminSessionStatus, LoginPayload } from '../types/admin';

export async function getAdminSession(): Promise<AdminSessionStatus> {
  const response = await api.get<AdminSessionStatus>('/admin/session');
  return response.data;
}

export async function bootstrapAdmin(password: string, bootstrapToken: string): Promise<void> {
  await api.post('/admin/bootstrap', { password, bootstrapToken });
}

export async function loginAdmin(payload: LoginPayload): Promise<void> {
  await api.post('/admin/login', payload);
}

export async function logoutAdmin(): Promise<void> {
  await api.post('/admin/logout');
}
