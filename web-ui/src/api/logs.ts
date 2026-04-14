import api from './index';
import type {
  AuditLogFilters,
  AuditLogItem,
  EventLogFilters,
  EventLogItem,
  LogListResponse,
} from '../types/logs';

export async function getAuditLogs(filters: AuditLogFilters = {}): Promise<LogListResponse<AuditLogItem>> {
  const response = await api.get<LogListResponse<AuditLogItem>>('/logs/audit', {
    params: filters,
  });
  return response.data;
}

export async function getEventLogs(filters: EventLogFilters = {}): Promise<LogListResponse<EventLogItem>> {
  const response = await api.get<LogListResponse<EventLogItem>>('/logs/events', {
    params: filters,
  });
  return response.data;
}
