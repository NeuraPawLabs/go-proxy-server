export interface LogListResponse<T> {
  items: T[];
  total: number;
  page: number;
  limit: number;
  pages: number;
  hasMore: boolean;
}

export interface AuditLogItem {
  id: number;
  occurredAt: string;
  actorType: string;
  actorId: string;
  action: string;
  targetType: string;
  targetId: string;
  status: string;
  sourceIp: string;
  userAgent: string;
  message: string;
  details?: Record<string, unknown>;
}

export interface EventLogItem {
  id: number;
  occurredAt: string;
  category: string;
  eventType: string;
  severity: string;
  source: string;
  message: string;
  details?: Record<string, unknown>;
}

export interface AuditLogFilters {
  page?: number;
  limit?: number;
  action?: string;
  status?: string;
  targetType?: string;
  search?: string;
  from?: string;
  to?: string;
}

export interface EventLogFilters {
  page?: number;
  limit?: number;
  category?: string;
  severity?: string;
  source?: string;
  eventType?: string;
  search?: string;
  from?: string;
  to?: string;
}
