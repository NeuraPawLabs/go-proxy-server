import React, { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Alert,
  AutoComplete,
  Button,
  Card,
  Col,
  Descriptions,
  Empty,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Row,
  Select,
  Space,
  Statistic,
  Switch,
  Table,
  Tag,
  Tooltip,
  Typography,
  message,
} from 'antd';
import type { FormInstance } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import {
  DeleteOutlined,
  EditOutlined,
  PlayCircleOutlined,
  PlusOutlined,
  QuestionCircleOutlined,
  ReloadOutlined,
  SafetyCertificateOutlined,
  SaveOutlined,
  StopOutlined,
} from '@ant-design/icons';
import {
  deleteTunnelClient,
  deleteTunnelRoute,
  generateTunnelServerCertificates,
  getManagedTunnelClientStatus,
  getManagedTunnelSessions,
  getTunnelClients,
  getTunnelRoutes,
  getTunnelServerStatus,
  saveManagedTunnelClientConfig,
  saveTunnelRoute,
  saveTunnelServerConfig,
  startManagedTunnelClient,
  startTunnelServer,
  stopManagedTunnelClient,
  stopTunnelServer,
  uploadManagedTunnelClientCA,
  uploadTunnelServerCertificates,
} from '../../api/tunnel';
import type {
  ManagedTunnelClientConfig,
  ManagedTunnelClientStatus,
  ManagedTunnelSession,
  TunnelClient,
  TunnelEngine,
  TunnelProtocol,
  TunnelRoute,
  TunnelServerCertificateState,
  TunnelServerConfig,
  TunnelServerEngineStatus,
  TunnelServerStatus,
} from '../../types/tunnel';
import { formatSessionAge, formatTransferSize } from '../../utils/tunnelSession';

const { Paragraph, Text, Title } = Typography;

type ClientStatusFilter = 'all' | 'online' | 'stale' | 'offline';
type TunnelMode = 'server' | 'client';
type DownloadKind = 'client-ca';

type UploadFilesState = {
  serverCert?: File;
  serverKey?: File;
  clientCa?: File;
};

type ClientCAUploadState = {
  clientCa?: File;
};

interface TunnelManagementProps {
  onOpenClient: (clientName: string) => void;
}

interface GenerateCertificateFormValues {
  commonName: string;
  hostsText: string;
  validDays: number;
}

interface RouteFormValues {
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

const defaultRouteFormValues: RouteFormValues = {
  clientName: '',
  name: '',
  protocol: 'tcp',
  targetAddr: '',
  publicPort: 0,
  ipWhitelist: [],
  udpIdleTimeoutSec: 60,
  udpMaxPayload: 1200,
  enabled: true,
};

const workbenchModeStorageKey = 'tunnelWorkbenchMode';

function getDefaultTunnelServerEngineStatus(engine: TunnelEngine): TunnelServerEngineStatus {
  return {
    running: false,
    engine,
    actualListenAddr: '',
    lastError: '',
    config: {
      engine,
      listenAddr: engine === 'quic' ? ':7443' : ':7000',
      publicBind: '0.0.0.0',
      clientEndpoint: '',
      token: '',
      autoStart: false,
      autoPortRangeStart: 0,
      autoPortRangeEnd: 0,
    },
  };
}

function getDefaultTunnelServerStatus(): TunnelServerStatus {
  return {
    classic: getDefaultTunnelServerEngineStatus('classic'),
    quic: getDefaultTunnelServerEngineStatus('quic'),
    certificates: {
      ready: false,
      managed: false,
      source: 'none',
      serverCertName: '',
      serverKeyName: '',
      clientCaName: '',
      canDownloadClientCa: false,
      message: '尚未配置 TLS 材料，可上传现有证书，或由后台直接生成。',
    },
  };
}

function getDefaultClientModeStatus(): ManagedTunnelClientStatus {
  return {
    running: false,
    engine: 'classic',
    connected: false,
    lastError: '',
    managedServerCaAvailable: false,
    certificates: {
      ready: false,
      source: 'none',
      caName: '',
      message: '尚未上传客户端 CA。',
    },
    config: {
      engine: 'classic',
      serverAddr: '',
      clientName: '',
      token: '',
      useManagedServerCa: false,
      serverName: '',
      insecureSkipVerify: false,
      allowInsecure: false,
      autoStart: false,
    },
    routes: [],
  };
}

function normalizeTunnelServerConfigValues(values: TunnelServerConfig): TunnelServerConfig {
  return {
    ...values,
    engine: values.engine || 'classic',
    autoPortRangeStart: values.autoPortRangeStart || 0,
    autoPortRangeEnd: values.autoPortRangeEnd || 0,
  };
}

function normalizeTunnelClientConfigValues(
  values: ManagedTunnelClientConfig,
): ManagedTunnelClientConfig {
  return {
    ...values,
    engine: values.engine || 'classic',
    serverAddr: values.serverAddr?.trim() || '',
    clientName: values.clientName?.trim() || '',
    token: values.token?.trim() || '',
    serverName: values.serverName?.trim() || '',
  };
}

function buildManagedTunnelClientPayload(
  values: ManagedTunnelClientConfig,
  status: ManagedTunnelClientStatus,
): ManagedTunnelClientConfig {
  const normalized = normalizeTunnelClientConfigValues(values);
  const useUploadedCA = !!status.certificates.ready;

  return {
    ...normalized,
    useManagedServerCa: !useUploadedCA && status.managedServerCaAvailable,
    serverName: '',
  };
}

function validateAutoPortRange(start?: number, end?: number): string | null {
  const hasStart = typeof start === 'number';
  const hasEnd = typeof end === 'number';

  if (!hasStart && !hasEnd) {
    return null;
  }
  if (!hasStart || !hasEnd) {
    return '请同时填写自动端口起始和结束';
  }
  if (start < 1 || start > 65535 || end < 1 || end > 65535) {
    return '端口范围需在 1-65535';
  }
  if (start > end) {
    return '起始端口不能大于结束端口';
  }
  return null;
}

function normalizeHostsInput(value: string): string[] {
  return value
    .split(/[\s,]+/)
    .map((item) => item.trim())
    .filter((item, index, list) => item !== '' && list.indexOf(item) === index);
}

function normalizeWhitelist(values: readonly string[]): string[] {
  const seen = new Set<string>();
  const normalized: string[] = [];

  for (const rawValue of values) {
    const value = rawValue.trim();
    if (!value || seen.has(value)) {
      continue;
    }
    seen.add(value);
    normalized.push(value);
  }

  return normalized;
}

function labelWithHint(label: string, hint: string): React.ReactNode {
  return (
    <Space size={6}>
      <span>{label}</span>
      <Tooltip title={hint}>
        <QuestionCircleOutlined style={{ color: '#8c8c8c' }} />
      </Tooltip>
    </Space>
  );
}

function getClientStatusMeta(client: TunnelClient): { label: string; color: string } {
  if (client.connected && !client.stale) {
    return { label: '在线', color: 'green' };
  }
  if (client.connected && client.stale) {
    return { label: '状态过期', color: 'orange' };
  }
  return { label: '离线', color: 'default' };
}

function getClientModeStatusMeta(status: ManagedTunnelClientStatus): { label: string; color: string } {
  if (!status.running) {
    return { label: '未启动', color: 'default' };
  }
  if (status.connected) {
    return { label: '已连接', color: 'green' };
  }
  return { label: '重连中', color: 'orange' };
}

function getCertificateSourceMeta(
  source: TunnelServerCertificateState['source'],
): { label: string; color: string } {
  switch (source) {
    case 'uploaded':
      return { label: '已上传', color: 'blue' };
    case 'generated':
      return { label: '后台生成', color: 'green' };
    case 'legacy-path':
      return { label: '旧版路径', color: 'orange' };
    default:
      return { label: '未配置', color: 'default' };
  }
}

function formatDateTime(value?: string): string {
  if (!value) {
    return '-';
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleString();
}

function triggerFileDownload(kind: DownloadKind) {
  const link = document.createElement('a');
  link.href = `/api/tunnel/server/files/${kind}`;
  link.style.display = 'none';
  document.body.appendChild(link);
  link.click();
  link.remove();
}

function extractListenPort(listenAddr: string): string {
  const addr = listenAddr.trim();
  if (addr === '') {
    return '';
  }
  if (addr.startsWith(':')) {
    return addr;
  }
  const ipv6Match = addr.match(/^\[.*\]:(\d+)$/);
  if (ipv6Match) {
    return `:${ipv6Match[1]}`;
  }
  const index = addr.lastIndexOf(':');
  if (index >= 0) {
    return addr.slice(index);
  }
  return '';
}

function buildSuggestedClientEndpoint(clientEndpoint: string, listenAddr: string): string {
  const preferred = clientEndpoint.trim();
  if (preferred !== '') {
    return preferred;
  }

  const addr = listenAddr.trim();
  const port = extractListenPort(addr);
  if (addr === '' || port === '') {
    return '';
  }
  if (addr.startsWith(':')) {
    return '';
  }

  const ipv6Match = addr.match(/^\[(.*)\]:(\d+)$/);
  if (ipv6Match) {
    const host = ipv6Match[1];
    if (host === '::' || host === '::1') {
      return '';
    }
    return addr;
  }

  const index = addr.lastIndexOf(':');
  if (index < 0) {
    return '';
  }
  const host = addr.slice(0, index);
  if (host === '0.0.0.0' || host === '127.0.0.1' || host === 'localhost') {
    return '';
  }
  return addr;
}

function describeServerPort(route: TunnelRoute): { primary: string; detail: string } {
  if (route.activePublicPort > 0) {
    return { primary: String(route.activePublicPort), detail: '当前生效' };
  }
  if (route.assignedPublicPort > 0) {
    return { primary: String(route.assignedPublicPort), detail: '已保留端口' };
  }
  if (route.publicPort > 0) {
    return { primary: String(route.publicPort), detail: '固定端口' };
  }
  return { primary: '自动分配', detail: '等待分配' };
}

function formatWhitelist(values: string[]): string {
  return values.length > 0 ? values.join(', ') : '未限制';
}

const TunnelManagement: React.FC<TunnelManagementProps> = ({ onOpenClient }) => {
  const [mode, setMode] = useState<TunnelMode>(() => {
    const stored = localStorage.getItem(workbenchModeStorageKey);
    return stored === 'client' ? 'client' : 'server';
  });
  const [clients, setClients] = useState<TunnelClient[]>([]);
  const [routes, setRoutes] = useState<TunnelRoute[]>([]);
  const [sessions, setSessions] = useState<ManagedTunnelSession[]>([]);
  const [serverStatus, setServerStatus] = useState<TunnelServerStatus | null>(null);
  const [clientModeStatus, setClientModeStatus] = useState<ManagedTunnelClientStatus | null>(null);
  const [searchKeyword, setSearchKeyword] = useState('');
  const [statusFilter, setStatusFilter] = useState<ClientStatusFilter>('all');
  const [loading, setLoading] = useState(false);
  const [serverSubmitting, setServerSubmitting] = useState(false);
  const [clientSubmitting, setClientSubmitting] = useState(false);
  const [uploadModalOpen, setUploadModalOpen] = useState(false);
  const [uploadSubmitting, setUploadSubmitting] = useState(false);
  const [uploadFiles, setUploadFiles] = useState<UploadFilesState>({});
  const [uploadInputSeed, setUploadInputSeed] = useState(0);
  const [clientCAUploadModalOpen, setClientCAUploadModalOpen] = useState(false);
  const [clientCAUploadSubmitting, setClientCAUploadSubmitting] = useState(false);
  const [clientCAUpload, setClientCAUpload] = useState<ClientCAUploadState>({});
  const [clientCAUploadSeed, setClientCAUploadSeed] = useState(0);
  const [generateModalOpen, setGenerateModalOpen] = useState(false);
  const [generateSubmitting, setGenerateSubmitting] = useState(false);
  const [routeModalOpen, setRouteModalOpen] = useState(false);
  const [routeSubmitting, setRouteSubmitting] = useState(false);
  const [editingRouteKey, setEditingRouteKey] = useState<string | null>(null);
  const [deletingClientName, setDeletingClientName] = useState<string | null>(null);
  const [messageApi, contextHolder] = message.useMessage();
  const [classicServerForm] = Form.useForm<TunnelServerConfig>();
  const [quicServerForm] = Form.useForm<TunnelServerConfig>();
  const [clientForm] = Form.useForm<ManagedTunnelClientConfig>();
  const [generateForm] = Form.useForm<GenerateCertificateFormValues>();
  const [routeForm] = Form.useForm<RouteFormValues>();

  const classicListenAddrValue = Form.useWatch('listenAddr', classicServerForm) ?? ':7000';
  const classicClientEndpointValue = Form.useWatch('clientEndpoint', classicServerForm) ?? '';
  const classicTokenValue = Form.useWatch('token', classicServerForm) ?? '';
  const classicAutoPortRangeStart = Form.useWatch('autoPortRangeStart', classicServerForm);
  const classicAutoPortRangeEnd = Form.useWatch('autoPortRangeEnd', classicServerForm);
  const quicListenAddrValue = Form.useWatch('listenAddr', quicServerForm) ?? ':7443';
  const quicClientEndpointValue = Form.useWatch('clientEndpoint', quicServerForm) ?? '';
  const quicTokenValue = Form.useWatch('token', quicServerForm) ?? '';
  const quicAutoPortRangeStart = Form.useWatch('autoPortRangeStart', quicServerForm);
  const quicAutoPortRangeEnd = Form.useWatch('autoPortRangeEnd', quicServerForm);
  const routeProtocol = (Form.useWatch('protocol', routeForm) ?? 'tcp') as TunnelProtocol;

  const clientServerAddr = Form.useWatch('serverAddr', clientForm) ?? '';
  const clientName = Form.useWatch('clientName', clientForm) ?? '';
  const clientToken = Form.useWatch('token', clientForm) ?? '';

  const applyServerStatus = useCallback(
    (status: TunnelServerStatus) => {
      setServerStatus(status);
      classicServerForm.setFieldsValue({
        ...status.classic.config,
        engine: 'classic',
        autoPortRangeStart: status.classic.config.autoPortRangeStart || undefined,
        autoPortRangeEnd: status.classic.config.autoPortRangeEnd || undefined,
      });
      quicServerForm.setFieldsValue({
        ...status.quic.config,
        engine: 'quic',
        autoPortRangeStart: status.quic.config.autoPortRangeStart || undefined,
        autoPortRangeEnd: status.quic.config.autoPortRangeEnd || undefined,
      });
    },
    [classicServerForm, quicServerForm],
  );

  const applyClientModeStatus = useCallback(
    (status: ManagedTunnelClientStatus) => {
      setClientModeStatus({
        ...status,
        config: {
          ...status.config,
          engine: status.config?.engine || 'classic',
        },
        certificates: status.certificates || getDefaultClientModeStatus().certificates,
        routes: status.routes || [],
      });
      clientForm.setFieldsValue({
        ...status.config,
        engine: status.config?.engine || 'classic',
      });
    },
    [clientForm],
  );

  const loadData = useCallback(async () => {
    setLoading(true);
    try {
      const [serverRes, clientRes, clientsRes, routesRes, sessionsRes] = await Promise.all([
        getTunnelServerStatus(),
        getManagedTunnelClientStatus(),
        getTunnelClients(),
        getTunnelRoutes(),
        getManagedTunnelSessions(),
      ]);
      applyServerStatus(serverRes.data);
      applyClientModeStatus(clientRes.data);
      setClients(clientsRes.data || []);
      setRoutes(routesRes.data || []);
      setSessions(sessionsRes.data || []);
    } catch (error) {
      console.error('Failed to load tunnel data:', error);
    } finally {
      setLoading(false);
    }
  }, [applyClientModeStatus, applyServerStatus]);

  useEffect(() => {
    void loadData();
  }, [loadData]);

  useEffect(() => {
    localStorage.setItem(workbenchModeStorageKey, mode);
  }, [mode]);

  const activeClientModeStatus = clientModeStatus || getDefaultClientModeStatus();
  const activeServerStatus = serverStatus || getDefaultTunnelServerStatus();
  const classicServerStatus = activeServerStatus.classic;
  const quicServerStatus = activeServerStatus.quic;
  const clientModeMeta = getClientModeStatusMeta(activeClientModeStatus);
  const certificateMeta = getCertificateSourceMeta(activeServerStatus.certificates.source || 'none');
  const classicServerAutoPortError = validateAutoPortRange(classicAutoPortRangeStart, classicAutoPortRangeEnd);
  const quicServerAutoPortError = validateAutoPortRange(quicAutoPortRangeStart, quicAutoPortRangeEnd);
  const clientCAReady = activeClientModeStatus.certificates.ready;
  const clientManagedCAReady = activeClientModeStatus.managedServerCaAvailable;
  const classicSuggestedClientEndpoint = useMemo(
    () => buildSuggestedClientEndpoint(classicClientEndpointValue, classicListenAddrValue),
    [classicClientEndpointValue, classicListenAddrValue],
  );
  const quicSuggestedClientEndpoint = useMemo(
    () => buildSuggestedClientEndpoint(quicClientEndpointValue, quicListenAddrValue),
    [quicClientEndpointValue, quicListenAddrValue],
  );
  const classicClientAccessCommand = useMemo(() => {
    const endpoint = classicSuggestedClientEndpoint || '<server:port>';
    const token = classicTokenValue.trim() || '<token>';
    return `go-proxy-server tunnel-client -engine classic -server ${endpoint} -token ${token} -client <client-name> -ca ca.pem`;
  }, [classicSuggestedClientEndpoint, classicTokenValue]);
  const quicClientAccessCommand = useMemo(() => {
    const endpoint = quicSuggestedClientEndpoint || '<server:port>';
    const token = quicTokenValue.trim() || '<token>';
    return `go-proxy-server tunnel-client -engine quic -server ${endpoint} -token ${token} -client <client-name> -ca ca.pem`;
  }, [quicSuggestedClientEndpoint, quicTokenValue]);

  const filteredClients = useMemo(() => {
    return clients.filter((client) => {
      const keyword = searchKeyword.trim().toLowerCase();
      const matchesSearch =
        keyword === '' ||
        client.name.toLowerCase().includes(keyword) ||
        client.remoteAddr.toLowerCase().includes(keyword);

      if (!matchesSearch) {
        return false;
      }

      switch (statusFilter) {
        case 'online':
          return client.connected && !client.stale;
        case 'stale':
          return client.connected && client.stale;
        case 'offline':
          return !client.connected;
        default:
          return true;
      }
    });
  }, [clients, searchKeyword, statusFilter]);

  const onlineClientCount = useMemo(
    () => clients.filter((client) => client.connected && !client.stale).length,
    [clients],
  );

  const staleClientCount = useMemo(
    () => clients.filter((client) => client.connected && client.stale).length,
    [clients],
  );

  const enabledRouteCount = useMemo(
    () => routes.filter((route) => route.enabled).length,
    [routes],
  );

  const activeRouteCount = useMemo(
    () => routes.filter((route) => route.activePublicPort > 0).length,
    [routes],
  );

  const routeSessionCounts = useMemo(() => {
    const counts = new Map<string, number>();
    sessions.forEach((session) => {
      const key = `${session.clientName}/${session.routeName}`;
      counts.set(key, (counts.get(key) || 0) + 1);
    });
    return counts;
  }, [sessions]);

  const clientModeSessions = useMemo(() => {
    const clientNameValue = activeClientModeStatus.config.clientName?.trim();
    if (!clientNameValue) {
      return [];
    }
    return sessions.filter((session) => session.clientName === clientNameValue);
  }, [activeClientModeStatus.config.clientName, sessions]);

  const clientOptions = useMemo(
    () => clients.map((client) => ({ value: client.name })),
    [clients],
  );

  const classicServerCanStart = !!classicTokenValue.trim() && !!activeServerStatus.certificates.ready && !classicServerAutoPortError;
  const quicServerCanStart = !!quicTokenValue.trim() && !!activeServerStatus.certificates.ready && !quicServerAutoPortError;
  const clientCanStart = useMemo(() => {
    if (!clientServerAddr.trim() || !clientName.trim() || !clientToken.trim()) {
      return false;
    }
    return activeClientModeStatus.certificates.ready || activeClientModeStatus.managedServerCaAvailable;
  }, [
    activeClientModeStatus.certificates.ready,
    activeClientModeStatus.managedServerCaAvailable,
    clientName,
    clientServerAddr,
    clientToken,
  ]);

  const closeRouteModal = useCallback(() => {
    setRouteModalOpen(false);
    setEditingRouteKey(null);
    routeForm.resetFields();
    routeForm.setFieldsValue(defaultRouteFormValues);
  }, [routeForm]);

  const openCreateRouteModal = useCallback(() => {
    setEditingRouteKey(null);
    routeForm.resetFields();
    routeForm.setFieldsValue(defaultRouteFormValues);
    setRouteModalOpen(true);
  }, [routeForm]);

  const handleEditRoute = useCallback(
    (route: TunnelRoute) => {
      setEditingRouteKey(`${route.clientName}/${route.name}`);
      routeForm.setFieldsValue({
        clientName: route.clientName,
        name: route.name,
        protocol: route.protocol,
        targetAddr: route.targetAddr,
        publicPort: route.publicPort,
        ipWhitelist: route.ipWhitelist || [],
        udpIdleTimeoutSec: route.udpIdleTimeoutSec,
        udpMaxPayload: route.udpMaxPayload,
        enabled: route.enabled,
      });
      setRouteModalOpen(true);
    },
    [routeForm],
  );

  const handleServerStart = async (engine: TunnelEngine, values: TunnelServerConfig) => {
    const normalized = normalizeTunnelServerConfigValues(values);
    const rangeError = validateAutoPortRange(
      normalized.autoPortRangeStart,
      normalized.autoPortRangeEnd,
    );
    if (rangeError) {
      messageApi.warning(rangeError);
      return;
    }

    setServerSubmitting(true);
    try {
      const response = await startTunnelServer(normalized);
      applyServerStatus(response.data);
      messageApi.success(`${engine.toUpperCase()} 服务端已启动`);
      await loadData();
    } catch (error) {
      console.error('Failed to start managed tunnel server:', error);
    } finally {
      setServerSubmitting(false);
    }
  };

  const handleServerSave = async (engine: TunnelEngine, values: TunnelServerConfig) => {
    const normalized = normalizeTunnelServerConfigValues(values);
    const rangeError = validateAutoPortRange(
      normalized.autoPortRangeStart,
      normalized.autoPortRangeEnd,
    );
    if (rangeError) {
      messageApi.warning(rangeError);
      return;
    }

    setServerSubmitting(true);
    try {
      const response = await saveTunnelServerConfig(normalized);
      applyServerStatus(response.data);
      messageApi.success(`${engine.toUpperCase()} 配置已保存`);
    } catch (error) {
      console.error('Failed to save managed tunnel server config:', error);
    } finally {
      setServerSubmitting(false);
    }
  };

  const handleServerStop = async (engine: TunnelEngine) => {
    setServerSubmitting(true);
    try {
      await stopTunnelServer(engine);
      messageApi.success(`${engine.toUpperCase()} 服务端已停止`);
      await loadData();
    } catch (error) {
      console.error('Failed to stop managed tunnel server:', error);
    } finally {
      setServerSubmitting(false);
    }
  };

  const handleClientSave = async (values: ManagedTunnelClientConfig) => {
    setClientSubmitting(true);
    try {
      const response = await saveManagedTunnelClientConfig(
        buildManagedTunnelClientPayload(values, activeClientModeStatus),
      );
      applyClientModeStatus(response.data);
      messageApi.success('客户端模式配置已保存');
    } catch (error) {
      console.error('Failed to save managed tunnel client config:', error);
    } finally {
      setClientSubmitting(false);
    }
  };

  const handleClientStart = async (values: ManagedTunnelClientConfig) => {
    setClientSubmitting(true);
    try {
      const response = await startManagedTunnelClient(
        buildManagedTunnelClientPayload(values, activeClientModeStatus),
      );
      applyClientModeStatus(response.data);
      messageApi.success('客户端模式已启动');
      await loadData();
    } catch (error) {
      console.error('Failed to start managed tunnel client:', error);
    } finally {
      setClientSubmitting(false);
    }
  };

  const handleClientStop = async () => {
    setClientSubmitting(true);
    try {
      await stopManagedTunnelClient();
      messageApi.success('客户端模式已停止');
      await loadData();
    } catch (error) {
      console.error('Failed to stop managed tunnel client:', error);
    } finally {
      setClientSubmitting(false);
    }
  };

  const handleUploadCertificates = async () => {
    if (!uploadFiles.serverCert || !uploadFiles.serverKey) {
      messageApi.warning('请先选择服务端证书和私钥');
      return;
    }

    setUploadSubmitting(true);
    try {
      const formData = new FormData();
      formData.append('serverCert', uploadFiles.serverCert);
      formData.append('serverKey', uploadFiles.serverKey);
      if (uploadFiles.clientCa) {
        formData.append('clientCa', uploadFiles.clientCa);
      }
      const response = await uploadTunnelServerCertificates(formData);
      applyServerStatus(response.data);
      setUploadModalOpen(false);
      setUploadFiles({});
      setUploadInputSeed((seed) => seed + 1);
      messageApi.success('证书材料已更新');
    } catch (error) {
      console.error('Failed to upload tunnel certificates:', error);
    } finally {
      setUploadSubmitting(false);
    }
  };

  const handleUploadClientCA = async () => {
    if (!clientCAUpload.clientCa) {
      messageApi.warning('请先选择客户端 CA 文件');
      return;
    }

    setClientCAUploadSubmitting(true);
    try {
      const formData = new FormData();
      formData.append('clientCa', clientCAUpload.clientCa);
      const response = await uploadManagedTunnelClientCA(formData);
      applyClientModeStatus(response.data);
      setClientCAUploadModalOpen(false);
      setClientCAUpload({});
      setClientCAUploadSeed((seed) => seed + 1);
      messageApi.success('客户端 CA 已上传');
    } catch (error) {
      console.error('Failed to upload managed tunnel client CA:', error);
    } finally {
      setClientCAUploadSubmitting(false);
    }
  };

  const handleGenerateCertificates = async (values: GenerateCertificateFormValues) => {
    setGenerateSubmitting(true);
    try {
      const response = await generateTunnelServerCertificates({
        commonName: values.commonName.trim(),
        hosts: normalizeHostsInput(values.hostsText),
        validDays: values.validDays,
      });
      applyServerStatus(response.data);
      setGenerateModalOpen(false);
      generateForm.resetFields();
      messageApi.success('证书已生成');
    } catch (error) {
      console.error('Failed to generate tunnel certificates:', error);
    } finally {
      setGenerateSubmitting(false);
    }
  };

  const handleSaveRoute = async (values: RouteFormValues) => {
    if (values.protocol === 'udp' && !quicServerStatus.running) {
      messageApi.warning('请先启动 QUIC 服务端，再创建或修改 UDP 路由');
      return;
    }
    setRouteSubmitting(true);
    try {
      await saveTunnelRoute({
        clientName: values.clientName.trim(),
        name: values.name.trim(),
        protocol: values.protocol,
        targetAddr: values.targetAddr.trim(),
        publicPort: values.publicPort || 0,
        ipWhitelist: normalizeWhitelist(values.ipWhitelist || []),
        udpIdleTimeoutSec: values.udpIdleTimeoutSec || 60,
        udpMaxPayload: values.udpMaxPayload || 1200,
        enabled: values.enabled,
      });
      messageApi.success(editingRouteKey ? '路由已更新' : '路由已创建');
      closeRouteModal();
      await loadData();
    } catch (error) {
      console.error('Failed to save tunnel route:', error);
    } finally {
      setRouteSubmitting(false);
    }
  };

  const handleToggleRoute = async (route: TunnelRoute, enabled: boolean) => {
    try {
      await saveTunnelRoute({
        clientName: route.clientName,
        name: route.name,
        protocol: route.protocol,
        targetAddr: route.targetAddr,
        publicPort: route.publicPort,
        ipWhitelist: route.ipWhitelist || [],
        udpIdleTimeoutSec: route.udpIdleTimeoutSec,
        udpMaxPayload: route.udpMaxPayload,
        enabled,
      });
      messageApi.success(enabled ? '路由已启用' : '路由已停用');
      await loadData();
    } catch (error) {
      console.error('Failed to toggle tunnel route:', error);
    }
  };

  const handleDeleteRoute = async (route: TunnelRoute) => {
    try {
      await deleteTunnelRoute(route.clientName, route.name);
      messageApi.success('路由已删除');
      await loadData();
    } catch (error) {
      console.error('Failed to delete tunnel route:', error);
    }
  };

  const handleDeleteClient = async (clientNameValue: string) => {
    setDeletingClientName(clientNameValue);
    try {
      await deleteTunnelClient(clientNameValue);
      messageApi.success('客户端资产已删除');
      await loadData();
    } catch (error) {
      console.error('Failed to delete tunnel client:', error);
    } finally {
      setDeletingClientName(null);
    }
  };

  const serverClientColumns: ColumnsType<TunnelClient> = [
    {
      title: '客户端',
      key: 'name',
      render: (_, client) => (
        <Space direction="vertical" size={2}>
          <Button type="link" style={{ padding: 0, height: 'auto' }} onClick={() => onOpenClient(client.name)}>
            {client.name}
          </Button>
          <Text type="secondary">{client.remoteAddr || '-'}</Text>
        </Space>
      ),
    },
    {
      title: '状态',
      key: 'status',
      render: (_, client) => {
        const meta = getClientStatusMeta(client);
        return <Tag color={meta.color}>{meta.label}</Tag>;
      },
    },
    {
      title: '路由',
      key: 'routes',
      render: (_, client) => (
        <Space size={8}>
          <Tag color="blue">总计 {client.routeCount}</Tag>
          <Tag color={client.activeRouteCount > 0 ? 'green' : 'default'}>
            生效 {client.activeRouteCount}
          </Tag>
        </Space>
      ),
    },
    {
      title: '最近心跳',
      dataIndex: 'lastSeenAt',
      key: 'lastSeenAt',
      render: (value) => formatDateTime(value),
    },
    {
      title: '操作',
      key: 'actions',
      render: (_, client) => (
        <Space size={8}>
          <Button size="small" onClick={() => onOpenClient(client.name)}>
            管理路由
          </Button>
          <Popconfirm
            title="删除客户端资产"
            description={`确认删除 ${client.name} 吗？离线资产及其路由会一并清理。`}
            okText="删除"
            cancelText="取消"
            okButtonProps={{ danger: true, loading: deletingClientName === client.name }}
            onConfirm={() => void handleDeleteClient(client.name)}
            disabled={client.connected}
          >
            <Button danger size="small" icon={<DeleteOutlined />} disabled={client.connected}>
              删除
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  const routeColumns: ColumnsType<TunnelRoute> = [
    {
      title: '路由',
      key: 'route',
      render: (_, route) => (
        <Space direction="vertical" size={2}>
          <Space size={8}>
            <Text strong>{route.name}</Text>
            <Tag color={route.protocol === 'udp' ? 'purple' : 'blue'}>{route.protocol.toUpperCase()}</Tag>
          </Space>
          <Text type="secondary">{route.clientName}</Text>
        </Space>
      ),
    },
    {
      title: '目标地址',
      dataIndex: 'targetAddr',
      key: 'targetAddr',
      render: (value) => <Text code>{value}</Text>,
    },
    {
      title: '连接端口',
      key: 'publicPort',
      render: (_, route) => {
        const portMeta = describeServerPort(route);
        return (
          <Space direction="vertical" size={2}>
            <Text strong>{portMeta.primary}</Text>
            <Text type="secondary">{portMeta.detail}</Text>
          </Space>
        );
      },
    },
    {
      title: '来源白名单',
      dataIndex: 'ipWhitelist',
      key: 'ipWhitelist',
      render: (value: string[]) => <Text type="secondary">{formatWhitelist(value || [])}</Text>,
    },
    {
      title: '实时连接',
      key: 'activeSessions',
      render: (_, route) => {
        const count = routeSessionCounts.get(`${route.clientName}/${route.name}`) || 0;
        return <Tag color={count > 0 ? 'green' : 'default'}>{count}</Tag>;
      },
    },
    {
      title: '启用',
      dataIndex: 'enabled',
      key: 'enabled',
      render: (enabled, route) => (
        <Switch size="small" checked={enabled} onChange={(checked) => void handleToggleRoute(route, checked)} />
      ),
    },
    {
      title: '最近错误',
      dataIndex: 'lastError',
      key: 'lastError',
      render: (value) =>
        value ? (
          <Tooltip title={value}>
            <Text type="danger" ellipsis style={{ maxWidth: 220 }}>
              {value}
            </Text>
          </Tooltip>
        ) : (
          <Text type="secondary">-</Text>
        ),
    },
    {
      title: '操作',
      key: 'actions',
      render: (_, route) => (
        <Space size={8}>
          <Button size="small" icon={<EditOutlined />} onClick={() => handleEditRoute(route)}>
            编辑
          </Button>
          <Popconfirm
            title="删除路由"
            description={`确认删除 ${route.clientName}/${route.name} 吗？`}
            okText="删除"
            cancelText="取消"
            okButtonProps={{ danger: true }}
            onConfirm={() => void handleDeleteRoute(route)}
          >
            <Button danger size="small" icon={<DeleteOutlined />}>
              删除
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  const sessionColumns: ColumnsType<ManagedTunnelSession> = [
    {
      title: '客户端 / 路由',
      key: 'route',
      render: (_, session) => (
        <Space direction="vertical" size={2}>
          <Space size={8}>
            <Text strong>{session.routeName}</Text>
            <Tag color={session.protocol === 'udp' ? 'purple' : 'blue'}>{session.protocol.toUpperCase()}</Tag>
            <Tag>{session.engine.toUpperCase()}</Tag>
          </Space>
          <Text type="secondary">{session.clientName}</Text>
        </Space>
      ),
    },
    {
      title: '来源地址',
      dataIndex: 'sourceAddr',
      key: 'sourceAddr',
      render: (value) => <Text code>{value}</Text>,
    },
    {
      title: '连接端口',
      dataIndex: 'publicPort',
      key: 'publicPort',
      render: (value) => <Text strong>{value || '-'}</Text>,
    },
    {
      title: '持续时长',
      dataIndex: 'openedAt',
      key: 'openedAt',
      render: (value) => formatSessionAge(value),
    },
    {
      title: '最近活动',
      dataIndex: 'lastActivityAt',
      key: 'lastActivityAt',
      render: (value) => formatDateTime(value),
    },
    {
      title: '流量',
      key: 'traffic',
      render: (_, session) => (
        <Space direction="vertical" size={2}>
          <Text>入站 {formatTransferSize(session.bytesFromPublic)}</Text>
          <Text type="secondary">出站 {formatTransferSize(session.bytesToPublic)}</Text>
          {session.protocol === 'udp' ? (
            <Text type="secondary">
              包数 {session.packetsFromPublic} / {session.packetsToPublic}
            </Text>
          ) : null}
        </Space>
      ),
    },
  ];

  const clientModeRouteColumns: ColumnsType<ManagedTunnelClientStatus['routes'][number]> = [
    {
      title: '路由名称',
      dataIndex: 'name',
      key: 'name',
      render: (value) => <Text strong>{value}</Text>,
    },
    {
      title: '目标地址',
      dataIndex: 'targetAddr',
      key: 'targetAddr',
      render: (value) => <Text code>{value}</Text>,
    },
    {
      title: '服务端端口',
      dataIndex: 'publicPort',
      key: 'publicPort',
      render: (value) => (value > 0 ? <Text strong>{value}</Text> : <Text type="secondary">自动分配</Text>),
    },
    {
      title: '状态',
      dataIndex: 'enabled',
      key: 'enabled',
      render: (enabled) => <Tag color={enabled ? 'green' : 'default'}>{enabled ? '启用' : '停用'}</Tag>,
    },
  ];

  const renderWorkbenchChooser = () => {
    const serverCardBg = mode === 'server' ? '#e6f4ff' : '#fafafa';
    const clientCardBg = mode === 'client' ? '#f6ffed' : '#fafafa';

    return (
      <Card
        bordered={false}
        style={{ boxShadow: '0 10px 30px rgba(15, 23, 42, 0.06)', marginBottom: 24 }}
        styles={{ body: { padding: 28 } }}
      >
        <Row gutter={[24, 24]} align="middle" justify="space-between">
          <Col xs={24} lg={14}>
            <Space direction="vertical" size={10}>
              <Tag color="geekblue" style={{ width: 'fit-content', marginInlineEnd: 0 }}>
                内网穿透工作台
              </Tag>
              <Title level={3} style={{ margin: 0 }}>
                服务端 / 客户端双模式
              </Title>
              <Text type="secondary">服务端统一管理入口、证书与路由；客户端负责本机接入与路由接收。</Text>
            </Space>
          </Col>
        </Row>
        <Row gutter={[16, 16]} style={{ marginTop: 24 }}>
          <Col xs={24} xl={12}>
            <Card
              hoverable
              onClick={() => setMode('server')}
              style={{ background: serverCardBg, borderColor: mode === 'server' ? '#91caff' : '#f0f0f0' }}
              styles={{ body: { padding: 20 } }}
            >
              <Space direction="vertical" size={10} style={{ width: '100%' }}>
                <Space align="center" style={{ justifyContent: 'space-between', width: '100%' }}>
                  <Title level={5} style={{ margin: 0 }}>
                    服务端模式
                  </Title>
                  <Tag color={classicServerStatus.running || quicServerStatus.running ? 'green' : 'default'}>
                    {classicServerStatus.running || quicServerStatus.running ? '运行中' : '未启动'}
                  </Tag>
                </Space>
                <Text type="secondary">面向公网入口，管理客户端资产、证书材料与暴露路由。</Text>
                <Space size={8} wrap>
                  <Tag>客户端 {clients.length}</Tag>
                  <Tag>路由 {routes.length}</Tag>
                  <Tag color={activeRouteCount > 0 ? 'green' : 'default'}>生效 {activeRouteCount}</Tag>
                </Space>
              </Space>
            </Card>
          </Col>
          <Col xs={24} xl={12}>
            <Card
              hoverable
              onClick={() => setMode('client')}
              style={{ background: clientCardBg, borderColor: mode === 'client' ? '#b7eb8f' : '#f0f0f0' }}
              styles={{ body: { padding: 20 } }}
            >
              <Space direction="vertical" size={10} style={{ width: '100%' }}>
                <Space align="center" style={{ justifyContent: 'space-between', width: '100%' }}>
                  <Title level={5} style={{ margin: 0 }}>
                    客户端模式
                  </Title>
                  <Tag color={clientModeMeta.color}>{clientModeMeta.label}</Tag>
                </Space>
                <Text type="secondary">面向本机接入，托管客户端进程、连接参数与开机自启策略。</Text>
                <Space size={8} wrap>
                  <Tag>已接收路由 {activeClientModeStatus.routes.length}</Tag>
                  <Tag color={activeClientModeStatus.connected ? 'green' : 'default'}>
                    {activeClientModeStatus.connected ? '链路正常' : '等待连接'}
                  </Tag>
                  <Tag>{activeClientModeStatus.config.autoStart ? '已启用自启动' : '手动启动'}</Tag>
                </Space>
              </Space>
            </Card>
          </Col>
        </Row>
      </Card>
    );
  };

  const renderServerConfigCard = (
    title: string,
    engine: TunnelEngine,
    form: FormInstance<TunnelServerConfig>,
    status: TunnelServerEngineStatus,
    canStart: boolean,
    autoPortError: string | null,
  ) => (
    <Card
      bordered={false}
      title={title}
      extra={
        <Space>
          <Button icon={<SaveOutlined />} loading={serverSubmitting} onClick={() => void form.submit()}>
            保存
          </Button>
          {status.running ? (
            <Button danger icon={<StopOutlined />} loading={serverSubmitting} onClick={() => void handleServerStop(engine)}>
              停止
            </Button>
          ) : (
            <Button
              type="primary"
              icon={<PlayCircleOutlined />}
              loading={serverSubmitting}
              disabled={!canStart}
              onClick={() => void handleServerStart(engine, form.getFieldsValue(true))}
            >
              启动
            </Button>
          )}
        </Space>
      }
      style={{ boxShadow: '0 10px 30px rgba(15, 23, 42, 0.06)' }}
    >
      <Form<TunnelServerConfig> layout="vertical" form={form} onFinish={(values) => void handleServerSave(engine, values)}>
        <Row gutter={16}>
          <Col xs={24} md={12}>
            <Form.Item
              name="listenAddr"
              label={labelWithHint('监听地址', '服务端控制面监听地址，支持 :7000 或 0.0.0.0:7000')}
              rules={[{ required: true, message: '请输入监听地址' }]}
            >
              <Input placeholder={engine === 'quic' ? ':7443' : ':7000'} />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item
              name="publicBind"
              label={labelWithHint('公网绑定 IP', '自动分配端口时，对外监听所绑定的地址')}
              rules={[{ required: true, message: '请输入公网绑定 IP' }]}
            >
              <Input placeholder="0.0.0.0" />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item
              name="clientEndpoint"
              label={labelWithHint('客户端接入地址', '提供给远端客户端的地址，复制命令会优先使用该地址')}
            >
              <Input placeholder={engine === 'quic' ? 'tunnel.example.com:7443' : 'tunnel.example.com:7000'} />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item
              name="token"
              label={labelWithHint('接入 Token', '对应引擎的服务端与客户端共享认证口令')}
              rules={[{ required: true, message: '请输入接入 Token' }]}
            >
              <Input.Password placeholder="请输入接入 Token" />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item
              name="autoPortRangeStart"
              label={labelWithHint('自动端口起始', '自动分配时，从该端口开始搜索可用公网端口')}
            >
              <InputNumber min={1} max={65535} style={{ width: '100%' }} placeholder="例如 30000" />
            </Form.Item>
          </Col>
          <Col xs={24} md={12}>
            <Form.Item
              name="autoPortRangeEnd"
              label={labelWithHint('自动端口结束', '建议与云安全组中预留的端口区间保持一致')}
            >
              <InputNumber min={1} max={65535} style={{ width: '100%' }} placeholder="例如 30999" />
            </Form.Item>
          </Col>
        </Row>
        <Form.Item name="engine" hidden>
          <Input />
        </Form.Item>
        <Form.Item name="autoStart" label={labelWithHint('开机自启', '管理平台启动后自动恢复该引擎服务端')} valuePropName="checked">
          <Switch />
        </Form.Item>
        <Descriptions column={1} size="small" bordered style={{ marginBottom: 16 }}>
          <Descriptions.Item label="状态">
            <Tag color={status.running ? 'green' : 'default'}>{status.running ? '运行中' : '未启动'}</Tag>
          </Descriptions.Item>
          <Descriptions.Item label="当前监听">
            {status.actualListenAddr || status.config.listenAddr || '-'}
          </Descriptions.Item>
        </Descriptions>
        {autoPortError && <Alert type="warning" showIcon style={{ marginBottom: 16 }} message={autoPortError} />}
        {status.lastError && <Alert type="error" showIcon style={{ marginBottom: 16 }} message={status.lastError} />}
      </Form>
    </Card>
  );

  const renderClientAccessCard = (
    title: string,
    command: string,
    running: boolean,
  ) => (
    <Card bordered={false} title={title} style={{ boxShadow: '0 10px 30px rgba(15, 23, 42, 0.06)' }}>
      <Space direction="vertical" size={12} style={{ width: '100%' }}>
        <Tag color={running ? 'green' : 'default'} style={{ width: 'fit-content' }}>
          {running ? '运行中' : '未启动'}
        </Tag>
        <Paragraph copyable={{ text: command }} style={{ marginBottom: 0, padding: 12, background: '#f5f5f5', borderRadius: 8 }}>
          <Text code>{command}</Text>
        </Paragraph>
      </Space>
    </Card>
  );

  const renderServerWorkbench = () => (
    <Space direction="vertical" size={24} style={{ width: '100%' }}>
      <Space style={{ width: '100%', justifyContent: 'flex-end' }}>
        <Button icon={<ReloadOutlined />} onClick={() => void loadData()} loading={loading}>
          刷新
        </Button>
      </Space>

      <Row gutter={[16, 16]}>
        <Col xs={24} sm={12} xl={6}>
          <Card bordered={false} style={{ boxShadow: '0 8px 24px rgba(15, 23, 42, 0.05)' }}>
            <Statistic title="Classic 服务" value={classicServerStatus.running ? '运行中' : '未启动'} valueStyle={{ fontSize: 24 }} />
            <Text type="secondary">{classicServerStatus.actualListenAddr || classicServerStatus.config.listenAddr || '-'}</Text>
          </Card>
        </Col>
        <Col xs={24} sm={12} xl={6}>
          <Card bordered={false} style={{ boxShadow: '0 8px 24px rgba(15, 23, 42, 0.05)' }}>
            <Statistic title="QUIC 服务" value={quicServerStatus.running ? '运行中' : '未启动'} valueStyle={{ fontSize: 24 }} />
            <Text type="secondary">{quicServerStatus.actualListenAddr || quicServerStatus.config.listenAddr || '-'}</Text>
          </Card>
        </Col>
        <Col xs={24} sm={12} xl={6}>
          <Card bordered={false} style={{ boxShadow: '0 8px 24px rgba(15, 23, 42, 0.05)' }}>
            <Statistic title="在线客户端" value={onlineClientCount} suffix={<Text type="secondary">/ {clients.length}</Text>} />
            <Text type="secondary">状态过期 {staleClientCount} 台</Text>
          </Card>
        </Col>
        <Col xs={24} sm={12} xl={6}>
          <Card bordered={false} style={{ boxShadow: '0 8px 24px rgba(15, 23, 42, 0.05)' }}>
            <Statistic title="生效路由" value={activeRouteCount} suffix={<Text type="secondary">/ {enabledRouteCount}</Text>} />
            <Text type="secondary">已启用 {enabledRouteCount} 条</Text>
          </Card>
        </Col>
        <Col xs={24} sm={12} xl={6}>
          <Card bordered={false} style={{ boxShadow: '0 8px 24px rgba(15, 23, 42, 0.05)' }}>
            <Statistic title="证书状态" value={certificateMeta.label} valueStyle={{ fontSize: 24 }} />
            <Tag color={certificateMeta.color}>{activeServerStatus.certificates.ready ? '可用' : '待补齐'}</Tag>
          </Card>
        </Col>
      </Row>

      <Row gutter={[16, 16]} align="stretch">
        <Col xs={24} xl={12}>
          {renderServerConfigCard(
            'Classic TCP 服务端',
            'classic',
            classicServerForm,
            classicServerStatus,
            classicServerCanStart,
            classicServerAutoPortError,
          )}
        </Col>
        <Col xs={24} xl={12}>
          {renderServerConfigCard(
            'QUIC 服务端',
            'quic',
            quicServerForm,
            quicServerStatus,
            quicServerCanStart,
            quicServerAutoPortError,
          )}
        </Col>
      </Row>

      <Row gutter={[16, 16]} align="stretch">
        <Col xs={24} xl={8}>
          <Space direction="vertical" size={16} style={{ width: '100%' }}>
            <Card
              bordered={false}
              title="证书材料"
              extra={<Tag color={certificateMeta.color}>{certificateMeta.label}</Tag>}
              style={{ boxShadow: '0 10px 30px rgba(15, 23, 42, 0.06)' }}
            >
              <Space direction="vertical" size={16} style={{ width: '100%' }}>
                <Descriptions column={1} size="small">
                  <Descriptions.Item label="状态">
                    <Tag color={activeServerStatus.certificates.ready ? 'green' : 'default'}>
                      {activeServerStatus.certificates.ready ? '就绪' : '未完成'}
                    </Tag>
                  </Descriptions.Item>
                  <Descriptions.Item label="服务端证书">{activeServerStatus.certificates.serverCertName || '-'}</Descriptions.Item>
                  <Descriptions.Item label="客户端 CA">{activeServerStatus.certificates.clientCaName || '-'}</Descriptions.Item>
                </Descriptions>
                {activeServerStatus.certificates.message && (
                  <Alert
                    type={activeServerStatus.certificates.ready ? 'info' : 'warning'}
                    showIcon
                    message={activeServerStatus.certificates.message}
                  />
                )}
                <Space wrap>
                  <Button onClick={() => setUploadModalOpen(true)}>上传证书</Button>
                  <Button icon={<SafetyCertificateOutlined />} onClick={() => setGenerateModalOpen(true)}>
                    后台生成
                  </Button>
                  <Button
                    disabled={!activeServerStatus.certificates.canDownloadClientCa}
                    onClick={() => triggerFileDownload('client-ca')}
                  >
                    下载 CA
                  </Button>
                </Space>
              </Space>
            </Card>
          </Space>
        </Col>

        <Col xs={24} xl={8}>
          {renderClientAccessCard('Classic 客户端接入', classicClientAccessCommand, classicServerStatus.running)}
        </Col>
        <Col xs={24} xl={8}>
          {renderClientAccessCard('QUIC 客户端接入', quicClientAccessCommand, quicServerStatus.running)}
        </Col>
      </Row>

      <Card
        bordered={false}
        title="客户端资产"
        extra={
          <Space wrap>
            <Input.Search
              allowClear
              placeholder="搜索客户端名称 / 地址"
              style={{ width: 240 }}
              value={searchKeyword}
              onChange={(event) => setSearchKeyword(event.target.value)}
            />
            <Select<ClientStatusFilter>
              style={{ width: 160 }}
              value={statusFilter}
              onChange={(value) => setStatusFilter(value)}
              options={[
                { value: 'all', label: '全部状态' },
                { value: 'online', label: '在线' },
                { value: 'stale', label: '状态过期' },
                { value: 'offline', label: '离线' },
              ]}
            />
          </Space>
        }
        style={{ boxShadow: '0 10px 30px rgba(15, 23, 42, 0.06)' }}
      >
        <Table<TunnelClient>
          rowKey={(record) => record.id}
          loading={loading}
          columns={serverClientColumns}
          dataSource={filteredClients}
          pagination={{ pageSize: 8, showSizeChanger: false }}
        />
      </Card>

      <Card
        bordered={false}
        title="路由编排"
        extra={
          <Space>
            <Button icon={<ReloadOutlined />} onClick={() => void loadData()} loading={loading}>
              刷新
            </Button>
            <Button type="primary" icon={<PlusOutlined />} onClick={openCreateRouteModal}>
              新建路由
            </Button>
          </Space>
        }
        style={{ boxShadow: '0 10px 30px rgba(15, 23, 42, 0.06)' }}
      >
        <Table<TunnelRoute>
          rowKey={(record) => `${record.clientName}/${record.name}`}
          loading={loading}
          columns={routeColumns}
          dataSource={routes}
          pagination={{ pageSize: 8, showSizeChanger: false }}
        />
      </Card>

      <Card
        bordered={false}
        title="实时连接"
        extra={
          <Space size={8}>
            <Tag color={sessions.length > 0 ? 'green' : 'default'}>{sessions.length} 条活跃会话</Tag>
            <Button icon={<ReloadOutlined />} onClick={() => void loadData()} loading={loading}>
              刷新
            </Button>
          </Space>
        }
        style={{ boxShadow: '0 10px 30px rgba(15, 23, 42, 0.06)' }}
      >
        {sessions.length > 0 ? (
          <Table<ManagedTunnelSession>
            rowKey={(record) => record.id}
            loading={loading}
            columns={sessionColumns}
            dataSource={sessions}
            pagination={{ pageSize: 8, showSizeChanger: false }}
          />
        ) : (
          <Empty description="当前没有活跃穿透连接" />
        )}
      </Card>
    </Space>
  );

  const renderClientWorkbench = () => (
    <Space direction="vertical" size={24} style={{ width: '100%' }}>
      <Row gutter={[16, 16]}>
        <Col xs={24} sm={12} xl={6}>
          <Card bordered={false} style={{ boxShadow: '0 8px 24px rgba(15, 23, 42, 0.05)' }}>
            <Statistic title="进程状态" value={clientModeMeta.label} valueStyle={{ color: '#1677ff', fontSize: 24 }} />
            <Tag color={clientModeMeta.color}>{activeClientModeStatus.running ? 'managed' : 'stopped'}</Tag>
          </Card>
        </Col>
        <Col xs={24} sm={12} xl={6}>
          <Card bordered={false} style={{ boxShadow: '0 8px 24px rgba(15, 23, 42, 0.05)' }}>
            <Statistic title="服务端连接" value={activeClientModeStatus.connected ? '已连接' : '未连接'} valueStyle={{ fontSize: 24 }} />
            <Text type="secondary">{activeClientModeStatus.config.serverAddr || '-'}</Text>
          </Card>
        </Col>
        <Col xs={24} sm={12} xl={6}>
          <Card bordered={false} style={{ boxShadow: '0 8px 24px rgba(15, 23, 42, 0.05)' }}>
            <Statistic title="实时连接" value={clientModeSessions.length} />
            <Text type="secondary">
              {activeClientModeStatus.config.clientName || '未配置客户端名'}
            </Text>
          </Card>
        </Col>
        <Col xs={24} sm={12} xl={6}>
          <Card bordered={false} style={{ boxShadow: '0 8px 24px rgba(15, 23, 42, 0.05)' }}>
            <Statistic title="已接收路由" value={activeClientModeStatus.routes.length} />
            <Text type="secondary">启用 {activeClientModeStatus.routes.filter((route) => route.enabled).length} 条</Text>
          </Card>
        </Col>
        <Col xs={24} sm={12} xl={6}>
          <Card bordered={false} style={{ boxShadow: '0 8px 24px rgba(15, 23, 42, 0.05)' }}>
            <Statistic title="启动策略" value={activeClientModeStatus.config.autoStart ? '开机自启' : '手动启动'} valueStyle={{ fontSize: 24 }} />
            <Text type="secondary">客户端名 {activeClientModeStatus.config.clientName || '-'}</Text>
          </Card>
        </Col>
      </Row>

      <Row gutter={[16, 16]} align="stretch">
        <Col xs={24}>
          <Card
            bordered={false}
            title="客户端配置"
            extra={
              <Space>
                <Button icon={<ReloadOutlined />} onClick={() => void loadData()} loading={loading}>
                  刷新
                </Button>
                <Button icon={<SaveOutlined />} loading={clientSubmitting} onClick={() => void clientForm.submit()}>
                  保存
                </Button>
                {activeClientModeStatus.running ? (
                  <Button danger icon={<StopOutlined />} loading={clientSubmitting} onClick={() => void handleClientStop()}>
                    停止
                  </Button>
                ) : (
                  <Button
                    type="primary"
                    icon={<PlayCircleOutlined />}
                    loading={clientSubmitting}
                    disabled={!clientCanStart}
                    onClick={() => void handleClientStart(clientForm.getFieldsValue(true))}
                  >
                    启动
                  </Button>
                )}
              </Space>
            }
            style={{ boxShadow: '0 10px 30px rgba(15, 23, 42, 0.06)' }}
          >
            <Form<ManagedTunnelClientConfig> layout="vertical" form={clientForm} onFinish={(values) => void handleClientSave(values)}>
              <Row gutter={16}>
                <Col xs={24} md={12}>
                  <Form.Item
                    name="engine"
                    label={labelWithHint('隧道引擎', 'classic 为当前 TCP 模式；quic 支持接收 TCP/UDP 路由')}
                    rules={[{ required: true, message: '请选择隧道引擎' }]}
                  >
                    <Select
                      options={[
                        { value: 'classic', label: 'Classic TCP' },
                        { value: 'quic', label: 'QUIC' },
                      ]}
                    />
                  </Form.Item>
                </Col>
                <Col xs={24} md={12}>
                  <Form.Item
                    name="serverAddr"
                    label={labelWithHint('服务端地址', '目标隧道服务端地址，例如 tunnel.example.com:7443')}
                    rules={[{ required: true, message: '请输入服务端地址' }]}
                  >
                    <Input placeholder="tunnel.example.com:7443" />
                  </Form.Item>
                </Col>
                <Col xs={24} md={12}>
                  <Form.Item
                    name="clientName"
                    label={labelWithHint('客户端名称', '作为资产唯一标识，建议使用节点名或主机名')}
                    rules={[{ required: true, message: '请输入客户端名称' }]}
                  >
                    <Input placeholder="edge-node-01" />
                  </Form.Item>
                </Col>
                <Col xs={24}>
                  <Form.Item
                    name="token"
                    label={labelWithHint('接入 Token', '与服务端模式使用相同 Token 完成认证')}
                    rules={[{ required: true, message: '请输入接入 Token' }]}
                  >
                    <Input.Password placeholder="请输入接入 Token" />
                  </Form.Item>
                </Col>
              </Row>

              <Row gutter={16}>
                <Col xs={24} md={12}>
                  <Form.Item label={labelWithHint('客户端 CA', '上传用于校验服务端证书的 CA 文件；未上传时会优先尝试复用本机托管 CA')}>
                    <Button onClick={() => setClientCAUploadModalOpen(true)}>
                      {clientCAReady ? '重新上传CA' : '上传CA'}
                    </Button>
                  </Form.Item>
                </Col>
                <Col xs={24} md={12}>
                  <Form.Item name="autoStart" label={labelWithHint('开机自启', '管理平台启动后自动恢复客户端模式')} valuePropName="checked">
                    <Switch />
                  </Form.Item>
                </Col>
              </Row>

              {!clientCAReady && !clientManagedCAReady && (
                <Alert
                  type="warning"
                  showIcon
                  style={{ marginBottom: 16 }}
                  message="当前未准备 CA，请先上传客户端 CA；若本机已启用服务端模式并准备证书，也会自动复用本机托管 CA。"
                />
              )}
              {activeClientModeStatus.certificates.message && clientCAReady && (
                <Alert
                  type="info"
                  showIcon
                  style={{ marginBottom: 16 }}
                  message={activeClientModeStatus.certificates.message}
                />
              )}
              {activeClientModeStatus.lastError && (
                <Alert type="error" showIcon style={{ marginBottom: 16 }} message={activeClientModeStatus.lastError} />
              )}
            </Form>
          </Card>
        </Col>
      </Row>

      <Card
        bordered={false}
        title="实时连接"
        extra={
          <Space size={8}>
            <Tag color={clientModeSessions.length > 0 ? 'green' : 'default'}>
              {clientModeSessions.length} 条活跃会话
            </Tag>
            <Button icon={<ReloadOutlined />} onClick={() => void loadData()} loading={loading}>
              刷新
            </Button>
          </Space>
        }
        style={{ boxShadow: '0 10px 30px rgba(15, 23, 42, 0.06)' }}
      >
        {clientModeSessions.length > 0 ? (
          <Table<ManagedTunnelSession>
            rowKey={(record) => record.id}
            loading={loading}
            columns={sessionColumns}
            dataSource={clientModeSessions}
            pagination={{ pageSize: 8, showSizeChanger: false }}
          />
        ) : (
          <Empty description="当前客户端没有活跃穿透连接" />
        )}
      </Card>

      <Card
        bordered={false}
        title="当前接收路由"
        extra={
          <Button icon={<ReloadOutlined />} onClick={() => void loadData()} loading={loading}>
            刷新
          </Button>
        }
        style={{ boxShadow: '0 10px 30px rgba(15, 23, 42, 0.06)' }}
      >
        {activeClientModeStatus.routes.length > 0 ? (
          <Table
            rowKey={(record) => record.name}
            loading={loading}
            columns={clientModeRouteColumns}
            dataSource={activeClientModeStatus.routes}
            pagination={false}
          />
        ) : (
          <Empty description="尚未接收到路由" />
        )}
      </Card>
    </Space>
  );

  return (
    <>
      {contextHolder}
      {renderWorkbenchChooser()}
      {mode === 'server' ? renderServerWorkbench() : renderClientWorkbench()}

      <Modal
        title="上传服务端证书"
        open={uploadModalOpen}
        onCancel={() => {
          setUploadModalOpen(false);
          setUploadFiles({});
          setUploadInputSeed((seed) => seed + 1);
        }}
        onOk={() => void handleUploadCertificates()}
        okText="提交"
        cancelText="取消"
        confirmLoading={uploadSubmitting}
      >
        <Space direction="vertical" size={16} style={{ width: '100%' }}>
          <Text type="secondary">支持上传服务端证书、私钥，以及可选的客户端 CA。</Text>
          <div key={`server-cert-${uploadInputSeed}`}>
            <Text strong>服务端证书</Text>
            <input
              type="file"
              accept=".crt,.pem,.cer"
              onChange={(event) => setUploadFiles((current) => ({ ...current, serverCert: event.target.files?.[0] }))}
            />
          </div>
          <div key={`server-key-${uploadInputSeed}`}>
            <Text strong>服务端私钥</Text>
            <input
              type="file"
              accept=".key,.pem"
              onChange={(event) => setUploadFiles((current) => ({ ...current, serverKey: event.target.files?.[0] }))}
            />
          </div>
          <div key={`client-ca-${uploadInputSeed}`}>
            <Text strong>客户端 CA（可选）</Text>
            <input
              type="file"
              accept=".crt,.pem,.cer"
              onChange={(event) => setUploadFiles((current) => ({ ...current, clientCa: event.target.files?.[0] }))}
            />
          </div>
        </Space>
      </Modal>

      <Modal
        title="后台生成证书"
        open={generateModalOpen}
        onCancel={() => {
          setGenerateModalOpen(false);
          generateForm.resetFields();
        }}
        onOk={() => void generateForm.submit()}
        okText="生成"
        cancelText="取消"
        confirmLoading={generateSubmitting}
      >
        <Form<GenerateCertificateFormValues>
          layout="vertical"
          form={generateForm}
          initialValues={{ validDays: 365, hostsText: '127.0.0.1' }}
          onFinish={(values) => void handleGenerateCertificates(values)}
        >
          <Form.Item name="commonName" label="证书名称" rules={[{ required: true, message: '请输入证书名称' }]}>
            <Input placeholder="tunnel.example.com" />
          </Form.Item>
          <Form.Item name="hostsText" label="SAN 列表" rules={[{ required: true, message: '请输入至少一个主机名或 IP' }]}>
            <Input.TextArea rows={3} placeholder="tunnel.example.com,127.0.0.1" />
          </Form.Item>
          <Form.Item name="validDays" label="有效期（天）" rules={[{ required: true, message: '请输入有效期' }]}>
            <InputNumber min={1} max={3650} style={{ width: '100%' }} />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="上传客户端 CA"
        open={clientCAUploadModalOpen}
        onCancel={() => {
          setClientCAUploadModalOpen(false);
          setClientCAUpload({});
          setClientCAUploadSeed((seed) => seed + 1);
        }}
        onOk={() => void handleUploadClientCA()}
        okText="上传"
        cancelText="取消"
        confirmLoading={clientCAUploadSubmitting}
      >
        <Space direction="vertical" size={16} style={{ width: '100%' }}>
          <Text type="secondary">上传用于客户端模式校验服务端证书的 CA 文件。</Text>
          <div key={`client-mode-ca-${clientCAUploadSeed}`}>
            <Text strong>客户端 CA</Text>
            <input
              type="file"
              accept=".crt,.pem,.cer"
              onChange={(event) => setClientCAUpload({ clientCa: event.target.files?.[0] })}
            />
          </div>
        </Space>
      </Modal>

      <Modal
        title={editingRouteKey ? '编辑路由' : '新建路由'}
        open={routeModalOpen}
        onCancel={closeRouteModal}
        onOk={() => void routeForm.submit()}
        okText={editingRouteKey ? '保存' : '创建'}
        cancelText="取消"
        confirmLoading={routeSubmitting}
        destroyOnHidden
      >
        <Form<RouteFormValues>
          layout="vertical"
          form={routeForm}
          initialValues={defaultRouteFormValues}
          onFinish={(values) => void handleSaveRoute(values)}
        >
          <Form.Item
            name="clientName"
            label={labelWithHint('客户端名称', '可选择已有客户端，也可直接输入新客户端名称')}
            rules={[{ required: true, message: '请输入客户端名称' }]}
          >
            <AutoComplete
              options={clientOptions}
              placeholder="edge-node-01"
              filterOption={(inputValue, option) =>
                String(option?.value || '')
                  .toLowerCase()
                  .includes(inputValue.toLowerCase())
              }
            />
          </Form.Item>
          <Form.Item
            name="name"
            label={labelWithHint('路由名称', '客户端内唯一标识，建议使用 mysql、ssh、redis 等简洁名称')}
            rules={[{ required: true, message: '请输入路由名称' }]}
          >
            <Input placeholder="mysql" />
          </Form.Item>
          <Form.Item
            name="protocol"
            label={labelWithHint('转发协议', 'Classic 引擎仅支持 TCP；QUIC 引擎支持 TCP 和 UDP')}
            rules={[{ required: true, message: '请选择转发协议' }]}
          >
            <Select<TunnelProtocol>
              options={[
                { value: 'tcp', label: 'TCP' },
                {
                  value: 'udp',
                  label: quicServerStatus.running ? (
                    'UDP'
                  ) : (
                    <Tooltip title="需先启动 QUIC 服务端">
                      <span>UDP</span>
                    </Tooltip>
                  ),
                  disabled: !quicServerStatus.running,
                },
              ]}
            />
          </Form.Item>
          <Form.Item
            name="targetAddr"
            label={labelWithHint(
              '目标地址',
              routeProtocol === 'udp'
                ? '支持 53、:53 或 127.0.0.1:53，端口将自动补全到本机回环地址'
                : '支持 3306、:3306 或 127.0.0.1:3306，端口将自动补全到本机回环地址',
            )}
            rules={[{ required: true, message: '请输入目标地址' }]}
          >
            <Input placeholder={routeProtocol === 'udp' ? '127.0.0.1:53' : '127.0.0.1:3306'} />
          </Form.Item>
          {routeProtocol === 'udp' ? (
            <Space direction="vertical" size={0} style={{ width: '100%' }}>
              <Form.Item
                name="udpIdleTimeoutSec"
                label={labelWithHint('UDP 空闲超时', '超过该时长无报文往来时，自动回收 UDP 会话')}
                rules={[{ required: true, message: '请输入 UDP 空闲超时' }]}
              >
                <InputNumber min={5} max={3600} style={{ width: '100%' }} addonAfter="秒" />
              </Form.Item>
              <Form.Item
                name="udpMaxPayload"
                label={labelWithHint('UDP 单包大小', '限制每个 UDP 报文的最大负载，建议保持 1200 左右以适配 QUIC')}
                rules={[{ required: true, message: '请输入 UDP 单包大小' }]}
              >
                <InputNumber min={256} max={65507} style={{ width: '100%' }} addonAfter="bytes" />
              </Form.Item>
            </Space>
          ) : null}
          <Form.Item
            name="publicPort"
            label={labelWithHint('固定公网端口', '留空或 0 表示自动分配；固定端口需已在安全组放行')}
          >
            <InputNumber min={0} max={65535} style={{ width: '100%' }} placeholder="0 表示自动分配" />
          </Form.Item>
          <Form.Item
            name="ipWhitelist"
            label={labelWithHint('来源 IP 白名单', '留空表示不限制，支持单 IP 或 CIDR')}
          >
            <Select mode="tags" tokenSeparators={[',', ' ']} placeholder="例如 203.0.113.10 或 10.0.0.0/24" />
          </Form.Item>
          <Form.Item name="enabled" label={labelWithHint('启用路由', '保存后立即下发到对应客户端')} valuePropName="checked">
            <Switch />
          </Form.Item>
        </Form>
      </Modal>
    </>
  );
};

export default TunnelManagement;
