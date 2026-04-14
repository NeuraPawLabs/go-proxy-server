export interface ApiResponse<T = unknown> {
  status?: string;
  data?: T;
  error?: string;
}

export interface SystemSettings {
  autostartEnabled: boolean;
  registryEnabled: boolean;
  autostartSupported: boolean;
  platform: string;
}

export interface TimeoutConfig {
  connect: number;
  idleRead: number;
  idleWrite: number;
}

export interface LimiterConfig {
  maxConcurrentConnections: number;
  maxConcurrentConnectionsPerIP: number;
}

export interface SecurityConfig {
  allowPrivateIPAccess: boolean;
}

export interface UnifiedConfig {
  timeout: TimeoutConfig;
  limiter: LimiterConfig;
  system: SystemSettings;
  security: SecurityConfig;
}
