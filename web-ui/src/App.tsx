import React, { useCallback, useEffect, useState } from 'react';
import { Button, Layout, Menu, Space, Spin, theme } from 'antd';
import type { MenuProps } from 'antd';
import {
  ApiOutlined,
  ControlOutlined,
  DashboardOutlined,
  FileTextOutlined,
  DeploymentUnitOutlined,
  LogoutOutlined,
  MenuFoldOutlined,
  MenuUnfoldOutlined,
  SafetyOutlined,
  SettingOutlined,
  UserOutlined,
} from '@ant-design/icons';
import Dashboard from './components/Dashboard';
import ProxyControl from './components/ProxyControl';
import UserManagement from './components/UserManagement';
import WhitelistManagement from './components/WhitelistManagement';
import ConfigManagement from './components/ConfigManagement';
import TunnelManagement from './components/TunnelManagement';
import TunnelClientDetail from './components/TunnelClientDetail';
import AdminAuth from './components/AdminAuth';
import ActivityLogs from './components/ActivityLogs';
import {
  getAdminSession,
  bootstrapAdmin,
  loginAdmin,
  logoutAdmin,
} from './api/admin';
import {
  ADMIN_AUTH_REQUIRED_EVENT,
  getApiErrorMessage,
} from './api/index';
import './App.css';
import type { LoginPayload } from './types/admin';

const { Header, Sider, Content } = Layout;

interface AuthState {
  loading: boolean;
  authenticated: boolean;
  bootstrapNeeded: boolean;
  geetestId?: string;
  captchaError?: string;
}

type AppViewState = {
  page: string;
  tunnelClient: string | null;
};

function parseViewStateFromHash(): AppViewState | null {
  const hash = window.location.hash.replace(/^#/, '');
  if (hash === '') {
    return null;
  }

  const normalized = hash.startsWith('/') ? hash : `/${hash}`;
  if (normalized.startsWith('/tunnel/client/')) {
    const clientName = decodeURIComponent(normalized.slice('/tunnel/client/'.length));
    if (clientName !== '') {
      return { page: 'tunnel-client-detail', tunnelClient: clientName };
    }
  }

  const page = normalized.slice(1);
  if (page !== '') {
    return { page, tunnelClient: null };
  }

  return null;
}

function buildHash(page: string, tunnelClient: string | null): string {
  if (page === 'tunnel-client-detail' && tunnelClient) {
    return `#/tunnel/client/${encodeURIComponent(tunnelClient)}`;
  }
  return `#/${page}`;
}

const App: React.FC = () => {
  const initialViewState = parseViewStateFromHash();
  const [collapsed, setCollapsed] = useState(false);
  const [selectedKey, setSelectedKey] = useState(
    () => initialViewState?.page || localStorage.getItem('selectedPage') || 'dashboard'
  );
  const [selectedTunnelClient, setSelectedTunnelClient] = useState<string | null>(
    () => initialViewState?.tunnelClient || null
  );
  const [authState, setAuthState] = useState<AuthState>({
    loading: true,
    authenticated: false,
    bootstrapNeeded: false,
  });
  const [authSubmitting, setAuthSubmitting] = useState(false);
  const [authError, setAuthError] = useState<string | null>(null);
  const {
    token: { colorBgContainer },
  } = theme.useToken();

  const menuItems: MenuProps['items'] = [
    {
      type: 'group',
      label: '总览',
      children: [
        { key: 'dashboard', icon: <DashboardOutlined />, label: '仪表盘' },
      ],
    },
    {
      type: 'group',
      label: '运行控制',
      children: [
        { key: 'proxy', icon: <ControlOutlined />, label: '代理控制' },
        { key: 'tunnel', icon: <DeploymentUnitOutlined />, label: '内网穿透' },
      ],
    },
    {
      type: 'group',
      label: '访问与安全',
      children: [
        { key: 'users', icon: <UserOutlined />, label: '用户管理' },
        { key: 'whitelist', icon: <SafetyOutlined />, label: 'IP 白名单' },
      ],
    },
    {
      type: 'group',
      label: '运维与审计',
      children: [
        { key: 'logs', icon: <FileTextOutlined />, label: '日志中心' },
        { key: 'config', icon: <SettingOutlined />, label: '系统配置' },
      ],
    },
  ];

  const refreshAdminSession = useCallback(async () => {
    try {
      const session = await getAdminSession();
      setAuthState({
        loading: false,
        authenticated: session.authenticated,
        bootstrapNeeded: session.bootstrapNeeded,
        geetestId: session.geetestId,
        captchaError: session.captchaError,
      });
      setAuthError(null);
    } catch (error) {
      setAuthState({
        loading: false,
        authenticated: false,
        bootstrapNeeded: false,
      });
      setAuthError(getApiErrorMessage(error, '加载管理后台认证状态失败'));
    }
  }, []);

  useEffect(() => {
    void refreshAdminSession();
  }, [refreshAdminSession]);

  useEffect(() => {
    const handleAuthRequired = () => {
      setAuthState((current) => ({
        loading: false,
        authenticated: false,
        bootstrapNeeded: current.bootstrapNeeded,
        geetestId: current.geetestId,
        captchaError: current.captchaError,
      }));
      setAuthError('登录状态已失效，请重新登录');
    };

    window.addEventListener(ADMIN_AUTH_REQUIRED_EVENT, handleAuthRequired);
    return () => window.removeEventListener(ADMIN_AUTH_REQUIRED_EVENT, handleAuthRequired);
  }, []);

  useEffect(() => {
    const handleHashChange = () => {
      const viewState = parseViewStateFromHash();
      if (!viewState) {
        return;
      }
      setSelectedKey(viewState.page);
      setSelectedTunnelClient(viewState.tunnelClient);
      if (viewState.page !== 'tunnel-client-detail') {
        localStorage.setItem('selectedPage', viewState.page);
      } else {
        localStorage.setItem('selectedPage', 'tunnel');
      }
    };

    window.addEventListener('hashchange', handleHashChange);
    return () => window.removeEventListener('hashchange', handleHashChange);
  }, []);

  useEffect(() => {
    const nextHash = buildHash(selectedKey, selectedTunnelClient);
    if (window.location.hash !== nextHash) {
      window.location.hash = nextHash;
    }
  }, [selectedKey, selectedTunnelClient]);

  const handleAuthSubmit = useCallback(async (payload: LoginPayload) => {
    setAuthSubmitting(true);
    setAuthError(null);

    try {
      if (authState.bootstrapNeeded) {
        await bootstrapAdmin(payload.password, payload.bootstrapToken ?? '');
      } else {
        await loginAdmin(payload);
      }

      setAuthState((current) => ({
        loading: false,
        authenticated: true,
        bootstrapNeeded: false,
        geetestId: current.geetestId,
        captchaError: current.captchaError,
      }));
    } catch (error) {
      setAuthError(
        getApiErrorMessage(
          error,
          authState.bootstrapNeeded ? '初始化管理后台失败' : '登录失败'
        )
      );
      throw error;
    } finally {
      setAuthSubmitting(false);
    }
  }, [authState.bootstrapNeeded]);

  const handleLogout = useCallback(async () => {
    try {
      await logoutAdmin();
    } catch (error) {
      console.warn('logout failed:', error);
    } finally {
      setAuthError(null);
      setAuthState((current) => ({
        loading: false,
        authenticated: false,
        bootstrapNeeded: false,
        geetestId: current.geetestId,
        captchaError: current.captchaError,
      }));
    }
  }, []);

  const openTunnelClientDetail = useCallback((clientName: string) => {
    setSelectedTunnelClient(clientName);
    setSelectedKey('tunnel-client-detail');
  }, []);

  const closeTunnelClientDetail = useCallback(() => {
    setSelectedTunnelClient(null);
    setSelectedKey('tunnel');
  }, []);

  const renderContent = () => {
    switch (selectedKey) {
      case 'dashboard':
        return <Dashboard />;
      case 'proxy':
        return <ProxyControl />;
      case 'users':
        return <UserManagement />;
      case 'whitelist':
        return <WhitelistManagement />;
      case 'config':
        return <ConfigManagement />;
      case 'logs':
        return <ActivityLogs />;
      case 'tunnel':
        return <TunnelManagement onOpenClient={openTunnelClientDetail} />;
      case 'tunnel-client-detail':
        return selectedTunnelClient ? (
          <TunnelClientDetail
            clientName={selectedTunnelClient}
            onBack={closeTunnelClientDetail}
            onOpenClient={openTunnelClientDetail}
          />
        ) : (
          <TunnelManagement onOpenClient={openTunnelClientDetail} />
        );
      default:
        return <Dashboard />;
    }
  };

  if (authState.loading) {
    return (
      <div className="app-loading-shell">
        <Spin size="large" tip="正在检查管理后台认证状态..." />
      </div>
    );
  }

  if (!authState.authenticated) {
    return (
      <AdminAuth
        bootstrapNeeded={authState.bootstrapNeeded}
        submitting={authSubmitting}
        error={authError}
        geetestId={authState.geetestId}
        captchaError={authState.captchaError}
        onSubmit={handleAuthSubmit}
      />
    );
  }

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Sider
        trigger={null}
        collapsible
        collapsed={collapsed}
        style={{
          overflow: 'auto',
          height: '100vh',
          position: 'fixed',
          left: 0,
          top: 0,
          bottom: 0,
        }}
      >
        <div
          style={{
            height: 64,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            padding: '0 16px',
            color: '#fff',
            fontSize: collapsed ? '20px' : '18px',
            fontWeight: 'bold',
            transition: 'all 0.2s',
          }}
        >
          <ApiOutlined style={{ fontSize: '24px', marginRight: collapsed ? 0 : '8px' }} />
          {!collapsed && <span>Proxy Server</span>}
        </div>
        <Menu
          theme="dark"
          mode="inline"
          selectedKeys={[selectedKey === 'tunnel-client-detail' ? 'tunnel' : selectedKey]}
          items={menuItems}
          onClick={({ key }) => {
            setSelectedKey(key);
            if (key !== 'tunnel-client-detail') {
              setSelectedTunnelClient(null);
            }
            localStorage.setItem('selectedPage', key === 'tunnel-client-detail' ? 'tunnel' : key);
          }}
        />
      </Sider>
      <Layout style={{ marginLeft: collapsed ? 80 : 200, transition: 'all 0.2s' }}>
        <Header
          style={{
            padding: '0 24px',
            background: colorBgContainer,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            boxShadow: '0 1px 4px rgba(0,21,41,.08)',
          }}
        >
          <Space>
            {React.createElement(collapsed ? MenuUnfoldOutlined : MenuFoldOutlined, {
              className: 'trigger',
              onClick: () => setCollapsed(!collapsed),
              style: { fontSize: '18px', cursor: 'pointer', marginRight: '24px' },
            })}
            <h1
              style={{
                margin: 0,
                fontSize: '20px',
                fontWeight: 600,
                color: '#1890ff',
              }}
            >
              Go Proxy Server 管理后台
            </h1>
          </Space>
          <Space>
            <Button icon={<LogoutOutlined />} onClick={() => void handleLogout()}>
              退出登录
            </Button>
          </Space>
        </Header>
        <Content
          style={{
            margin: '24px',
            padding: '24px',
            background: '#f0f2f5',
            minHeight: 'calc(100vh - 112px)',
          }}
        >
          <div style={{ maxWidth: '1600px', margin: '0 auto' }}>
            {renderContent()}
          </div>
        </Content>
      </Layout>
    </Layout>
  );
};

export default App;
