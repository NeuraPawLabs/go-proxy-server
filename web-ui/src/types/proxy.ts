export interface ProxyServerStatus {
  running: boolean;
  port: number;
  bindListen: boolean;
  autoStart: boolean;
}

export interface ProxyStatus {
  socks5: ProxyServerStatus;
  http: ProxyServerStatus;
}

export interface ProxyStartRequest {
  type: 'socks5' | 'http';
  port: number;
  bindListen: boolean;
}

export interface ProxyStopRequest {
  type: 'socks5' | 'http';
}

export interface ProxyConfigRequest {
  type: 'socks5' | 'http';
  port: number;
  bindListen: boolean;
  autoStart: boolean;
}
