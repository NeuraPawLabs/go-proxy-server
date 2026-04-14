import api from "./index";
import type {
  ManagedTunnelClientConfig,
  ManagedTunnelClientStatus,
  ManagedTunnelSession,
  TunnelClient,
  TunnelRoute,
  TunnelRouteUpsertRequest,
  TunnelServerConfig,
  TunnelServerGenerateCertificatesRequest,
  TunnelServerStatus,
} from "../types/tunnel";

export const getTunnelServerStatus = () =>
  api.get<TunnelServerStatus>("/tunnel/server");

export const saveTunnelServerConfig = (data: TunnelServerConfig) =>
  api.post<TunnelServerStatus>("/tunnel/server/config", data);

export const startTunnelServer = (data: TunnelServerConfig) =>
  api.post<TunnelServerStatus>("/tunnel/server/start", data);

export const stopTunnelServer = (engine: TunnelServerConfig["engine"]) =>
  api.post("/tunnel/server/stop", { engine });

export const uploadTunnelServerCertificates = (data: FormData) =>
  api.post<TunnelServerStatus>("/tunnel/server/certificates/upload", data, {
    headers: {
      "Content-Type": "multipart/form-data",
    },
  });

export const generateTunnelServerCertificates = (
  data: TunnelServerGenerateCertificatesRequest,
) => api.post<TunnelServerStatus>("/tunnel/server/certificates/generate", data);

export const getTunnelClients = () =>
  api.get<TunnelClient[]>("/tunnel/clients");

export const getManagedTunnelClientStatus = () =>
  api.get<ManagedTunnelClientStatus>("/tunnel/client");

export const saveManagedTunnelClientConfig = (data: ManagedTunnelClientConfig) =>
  api.post<ManagedTunnelClientStatus>("/tunnel/client/config", data);

export const uploadManagedTunnelClientCA = (data: FormData) =>
  api.post<ManagedTunnelClientStatus>("/tunnel/client/ca", data, {
    headers: {
      "Content-Type": "multipart/form-data",
    },
  });

export const startManagedTunnelClient = (data: ManagedTunnelClientConfig) =>
  api.post<ManagedTunnelClientStatus>("/tunnel/client/start", data);

export const stopManagedTunnelClient = () => api.post("/tunnel/client/stop");

export const deleteTunnelClient = (clientName: string) =>
  api.delete("/tunnel/clients", { data: { clientName } });

export const getTunnelRoutes = () => api.get<TunnelRoute[]>("/tunnel/routes");

export const saveTunnelRoute = (data: TunnelRouteUpsertRequest) =>
  api.post("/tunnel/routes", data);

export const deleteTunnelRoute = (clientName: string, name: string) =>
  api.delete("/tunnel/routes", { data: { clientName, name } });

export const getManagedTunnelSessions = (params?: { clientName?: string; routeName?: string }) =>
  api.get<ManagedTunnelSession[]>("/tunnel/sessions", { params });
