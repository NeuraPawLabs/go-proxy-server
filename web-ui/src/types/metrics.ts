export interface MetricsSnapshot {
  timestamp: number;
  activeConnections: number;
  maxActiveConnections: number;
  totalConnections: number;
  bytesReceived: number;
  bytesSent: number;
  uploadSpeed: number;
  downloadSpeed: number;
  maxUploadSpeed: number;
  maxDownloadSpeed: number;
  errorCount: number;
  uptime: number;
}

export interface MetricsHistory {
  ID: number;
  CreatedAt: string;
  UpdatedAt: string;
  DeletedAt: string | null;
  Timestamp: number;
  ActiveConnections: number;
  TotalConnections: number;
  BytesReceived: number;
  BytesSent: number;
  UploadSpeed: number;
  DownloadSpeed: number;
  MaxUploadSpeed: number;
  MaxDownloadSpeed: number;
  ErrorCount: number;
}
