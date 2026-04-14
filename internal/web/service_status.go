package web

type statusResponse struct {
	Socks5 proxyStatusResponse `json:"socks5"`
	HTTP   proxyStatusResponse `json:"http"`
}

type proxyStatusResponse struct {
	Running    bool `json:"running"`
	Port       int  `json:"port"`
	BindListen bool `json:"bindListen"`
	AutoStart  bool `json:"autoStart"`
}

func (wm *Manager) buildStatusResponse() statusResponse {
	return statusResponse{
		Socks5: wm.socksServer.snapshot(),
		HTTP:   wm.httpServer.snapshot(),
	}
}

func (server *ProxyServer) snapshot() proxyStatusResponse {
	return proxyStatusResponse{
		Running:    server.Running.Load(),
		Port:       server.Port,
		BindListen: server.BindListen,
		AutoStart:  server.AutoStart,
	}
}
