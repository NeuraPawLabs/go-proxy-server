export type TunnelEngine = 'classic' | 'quic';
export type TunnelProtocol = 'tcp' | 'udp';

export interface TunnelClient {
  id: number;
  name: string;
  remoteAddr: string;
  engine: TunnelEngine;
  connected: boolean;
  stale: boolean;
  lastSeenAt?: string;
  routeCount: number;
  activeRouteCount: number;
}

export interface TunnelRoute {
  id: number;
  clientName: string;
  name: string;
  protocol: TunnelProtocol;
  targetAddr: string;
  publicPort: number;
  ipWhitelist: string[];
  udpIdleTimeoutSec: number;
  udpMaxPayload: number;
  assignedPublicPort: number;
  activePublicPort: number;
  enabled: boolean;
  lastError: string;
  updatedAt: string;
}

export interface TunnelRouteUpsertRequest {
  clientName: string;
  name: string;
  protocol: TunnelProtocol;
  targetAddr: string;
  publicPort: number;
  ipWhitelist: string[];
  udpIdleTimeoutSec: number;
  udpMaxPayload: number;
  enabled: boolean;
}

export interface TunnelServerConfig {
  engine: TunnelEngine;
  listenAddr: string;
  publicBind: string;
  clientEndpoint: string;
  token: string;
  autoStart: boolean;
  autoPortRangeStart?: number;
  autoPortRangeEnd?: number;
}

export interface TunnelServerCertificateState {
  ready: boolean;
  managed: boolean;
  source: "none" | "uploaded" | "generated" | "legacy-path" | string;
  serverCertName: string;
  serverKeyName: string;
  clientCaName: string;
  canDownloadClientCa: boolean;
  updatedAt?: string;
  message: string;
}

export interface TunnelServerGenerateCertificatesRequest {
  commonName: string;
  hosts: string[];
  validDays: number;
}

export interface TunnelServerStatus {
  classic: TunnelServerEngineStatus;
  quic: TunnelServerEngineStatus;
  certificates: TunnelServerCertificateState;
}

export interface TunnelServerEngineStatus {
  running: boolean;
  engine: TunnelEngine;
  actualListenAddr: string;
  lastError: string;
  config: TunnelServerConfig;
}

export interface ManagedTunnelClientConfig {
  engine: TunnelEngine;
  serverAddr: string;
  clientName: string;
  token: string;
  useManagedServerCa: boolean;
  serverName: string;
  insecureSkipVerify: boolean;
  allowInsecure: boolean;
  autoStart: boolean;
}

export interface ManagedTunnelClientCertificateState {
  ready: boolean;
  source: "none" | "uploaded" | string;
  caName: string;
  updatedAt?: string;
  message: string;
}

export interface ManagedTunnelClientRoute {
  name: string;
  targetAddr: string;
  publicPort: number;
  enabled: boolean;
  protocol: TunnelProtocol;
  udpIdleTimeoutSec: number;
  udpMaxPayload: number;
}

export interface ManagedTunnelClientStatus {
  running: boolean;
  engine: TunnelEngine;
  connected: boolean;
  lastError: string;
  connectedAt?: string;
  effectiveCaFile?: string;
  managedServerCaAvailable: boolean;
  certificates: ManagedTunnelClientCertificateState;
  config: ManagedTunnelClientConfig;
  routes: ManagedTunnelClientRoute[];
}

export interface ManagedTunnelSession {
  id: string;
  engine: TunnelEngine;
  protocol: TunnelProtocol;
  clientName: string;
  routeName: string;
  publicPort: number;
  targetAddr: string;
  sourceAddr: string;
  openedAt: string;
  lastActivityAt: string;
  closedAt?: string;
  bytesFromPublic: number;
  bytesToPublic: number;
  packetsFromPublic: number;
  packetsToPublic: number;
}
