import api from './index';
import type { MetricsSnapshot, MetricsHistory } from '../types/metrics';

export async function getRealtimeMetrics(): Promise<MetricsSnapshot> {
  const response = await api.get<MetricsSnapshot>('/metrics/realtime');
  return response.data;
}

export async function getMetricsHistory(
  startTime?: number,
  endTime?: number,
  limit?: number,
  downsample?: boolean
): Promise<MetricsHistory[]> {
  const response = await api.get<MetricsHistory[]>('/metrics/history', {
    params: {
      startTime,
      endTime,
      limit,
      downsample: downsample ? true : undefined,
    },
  });
  return response.data;
}
