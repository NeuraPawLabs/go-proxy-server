import React, { useState, useEffect } from 'react';
import { Row, Col, Card, Statistic, Typography, Descriptions, Tag, Space } from 'antd';
import {
  ApiOutlined,
  DeploymentUnitOutlined,
  UserOutlined,
  SafetyOutlined,
  ThunderboltOutlined,
  CloudUploadOutlined,
  CloudDownloadOutlined,
  LinkOutlined,
  WarningOutlined,
} from '@ant-design/icons';
import { Line } from '@ant-design/charts';
import { getProxyStatus } from '../../api/proxy';
import { getUsers } from '../../api/user';
import { getWhitelist } from '../../api/whitelist';
import { getRealtimeMetrics, getMetricsHistory } from '../../api/metrics';
import { getTunnelClients, getTunnelRoutes, getTunnelServerStatus } from '../../api/tunnel';
import type { ProxyStatus } from '../../types/proxy';
import type { MetricsSnapshot, MetricsHistory } from '../../types/metrics';
import type { TunnelClient, TunnelRoute, TunnelServerStatus } from '../../types/tunnel';

const { Title, Text } = Typography;

interface ChartTooltipItem {
  value: number;
  color: string;
  name: string;
}

function boolTag(value?: boolean): React.ReactNode {
  return <Tag color={value ? 'success' : 'default'}>{value ? '开' : '关'}</Tag>;
}

function statusTag(value?: boolean): React.ReactNode {
  return <Tag color={value ? 'success' : 'default'}>{value ? '运行中' : '已停'}</Tag>;
}

const Dashboard: React.FC = () => {
  const [proxyStatus, setProxyStatus] = useState<ProxyStatus | null>(null);
  const [userCount, setUserCount] = useState(0);
  const [whitelistCount, setWhitelistCount] = useState(0);
  const [metrics, setMetrics] = useState<MetricsSnapshot | null>(null);
  const [metricsHistory, setMetricsHistory] = useState<MetricsHistory[]>([]);
  const [tunnelStatus, setTunnelStatus] = useState<TunnelServerStatus | null>(null);
  const [tunnelClients, setTunnelClients] = useState<TunnelClient[]>([]);
  const [tunnelRoutes, setTunnelRoutes] = useState<TunnelRoute[]>([]);
  const [loading, setLoading] = useState(true);

  const formatBytes = (bytes: number): string => {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return (bytes / Math.pow(k, i)).toFixed(2) + ' ' + sizes[i];
  };

  const formatSpeed = (bytesPerSec: number): string => {
    return formatBytes(bytesPerSec) + '/s';
  };

  // Get optimal unit based on maximum speed value
  const getOptimalSpeedUnit = (data: typeof bandwidthData): { unit: string; divisor: number; decimals: number } => {
    if (data.length === 0) return { unit: 'B/s', divisor: 1, decimals: 0 };

    const maxValue = Math.max(...data.map(d => d.value));

    if (maxValue === 0) return { unit: 'B/s', divisor: 1, decimals: 0 };
    if (maxValue < 1024) return { unit: 'B/s', divisor: 1, decimals: 0 };
    if (maxValue < 1024 * 1024) return { unit: 'KB/s', divisor: 1024, decimals: 1 };
    if (maxValue < 1024 * 1024 * 1024) return { unit: 'MB/s', divisor: 1024 * 1024, decimals: 2 };
    return { unit: 'GB/s', divisor: 1024 * 1024 * 1024, decimals: 2 };
  };

  const formatUptime = (seconds: number): string => {
    const days = Math.floor(seconds / 86400);
    const hours = Math.floor((seconds % 86400) / 3600);
    const minutes = Math.floor((seconds % 3600) / 60);
    if (days > 0) return `${days}天 ${hours}小时`;
    if (hours > 0) return `${hours}小时 ${minutes}分钟`;
    return `${minutes}分钟`;
  };

  const loadData = async (isInitialLoad = false) => {
    try {
      if (isInitialLoad) {
        setLoading(true);
      }
      const [statusRes, usersRes, whitelistRes, metricsRes] = await Promise.all([
        getProxyStatus(),
        getUsers(),
        getWhitelist(),
        getRealtimeMetrics(),
      ]);
      const [tunnelStatusRes, tunnelClientsRes, tunnelRoutesRes] = await Promise.all([
        getTunnelServerStatus(),
        getTunnelClients(),
        getTunnelRoutes(),
      ]);
      setProxyStatus(statusRes.data);
      setUserCount(usersRes.data.length);
      setWhitelistCount(whitelistRes.data.length);
      setMetrics(metricsRes);
      setTunnelStatus(tunnelStatusRes.data);
      setTunnelClients(tunnelClientsRes.data);
      setTunnelRoutes(tunnelRoutesRes.data);
    } catch (error) {
      console.error('Failed to load dashboard data:', error);
    } finally {
      if (isInitialLoad) {
        setLoading(false);
      }
    }
  };

  const loadHistory = async () => {
    try {
      const endTime = Math.floor(Date.now() / 1000);
      const startTime = endTime - 3600; // Last hour
      const history = await getMetricsHistory(startTime, endTime, 60, true); // Enable downsampling
      setMetricsHistory(history);
    } catch (error) {
      console.error('Failed to load metrics history:', error);
    }
  };

  useEffect(() => {
    loadData(true); // Initial load with loading state
    loadHistory();
    const interval = setInterval(() => {
      loadData(false); // Subsequent updates without loading state
      loadHistory();
    }, 5000); // Update every 5 seconds
    return () => clearInterval(interval);
  }, []);

  const runningProxies = [
    proxyStatus?.socks5?.running,
    proxyStatus?.http?.running,
  ].filter(Boolean).length;
  const onlineTunnelClients = tunnelClients.filter((client) => client.connected && !client.stale).length;
  const enabledTunnelRoutes = tunnelRoutes.filter((route) => route.enabled).length;
  const tunnelRunning = !!(tunnelStatus?.classic.running || tunnelStatus?.quic.running);
  const tunnelListenSummary = [tunnelStatus?.classic.actualListenAddr, tunnelStatus?.quic.actualListenAddr]
    .filter((value) => Boolean(value))
    .join(' / ');

  // Prepare chart data
  const bandwidthData = metricsHistory.map((h) => ([
    {
      time: new Date(h.Timestamp * 1000).toLocaleTimeString(),
      value: h.UploadSpeed, // Keep as bytes/sec for accurate formatting
      type: '上传速度',
    },
    {
      time: new Date(h.Timestamp * 1000).toLocaleTimeString(),
      value: h.DownloadSpeed, // Keep as bytes/sec for accurate formatting
      type: '下载速度',
    },
  ])).flat();

  const connectionsData = metricsHistory.map((h) => ({
    time: new Date(h.Timestamp * 1000).toLocaleTimeString(),
    value: h.ActiveConnections,
  }));

  // Get optimal unit for bandwidth chart
  const speedUnit = getOptimalSpeedUnit(bandwidthData);

  const bandwidthConfig = {
    data: bandwidthData,
    xField: 'time',
    yField: 'value',
    seriesField: 'type',
    shapeField: 'smooth',
    animation: false,
    colorField: 'type',
    scale: {
      color: {
        range: ['#10b981', '#3b82f6'], // Vibrant green for upload, blue for download
      },
      y: {
        nice: true,
      },
    },
    axis: {
      y: {
        labelFormatter: (v: number) => {
          return `${(v / speedUnit.divisor).toFixed(speedUnit.decimals)} ${speedUnit.unit}`;
        },
      },
    },
    interaction: {
      tooltip: {
        render: (
          _event: unknown,
          {
            title,
            items,
          }: { title?: string; items?: ChartTooltipItem[] },
        ) => {
          if (!items || items.length === 0) return '';
          return `
            <div style="padding: 8px 12px;">
              <div style="margin-bottom: 8px; font-weight: 500;">${title}</div>
              ${items.map((item) => {
                const speed = formatSpeed(item.value);
                return `
                  <div style="display: flex; align-items: center; margin-bottom: 4px;">
                    <span style="display: inline-block; width: 8px; height: 8px; border-radius: 50%; background: ${item.color}; margin-right: 8px;"></span>
                    <span style="margin-right: 8px;">${item.name}:</span>
                    <span style="font-weight: 500;">${speed}</span>
                  </div>
                `;
              }).join('')}
            </div>
          `;
        },
      },
    },
    style: {
      lineWidth: 2,
    },
  };

  const connectionsConfig = {
    data: connectionsData,
    xField: 'time',
    yField: 'value',
    smooth: true,
    color: '#5B8FF9',
    animation: false, // Disable animation to prevent flashing on data updates
    yAxis: {
      label: {
        formatter: (v: string) => `${v} 个`,
      },
    },
  };

  const panelCardStyle = {
    borderRadius: 14,
    border: '1px solid #e6ebf2',
    boxShadow: '0 6px 18px rgba(15, 23, 42, 0.04)',
  } as const;

  const summaryCardStyle = {
    ...panelCardStyle,
    minHeight: 120,
  } as const;

  const sectionTitleStyle = {
    margin: '30px 0 14px',
    fontSize: 14,
    fontWeight: 600,
    color: '#334155',
    letterSpacing: 0.2,
    display: 'flex',
    alignItems: 'center',
    gap: 10,
  } as const;

  const sectionLineStyle = {
    flex: 1,
    height: 1,
    background: '#e8edf3',
  } as const;

  const compactCardStyles = {
    header: {
      minHeight: 46,
      padding: '0 18px',
      borderBottom: '1px solid #eef2f6',
    },
    body: {
      padding: 18,
    },
  } as const;

  const summaryCardBodyStyle = {
    padding: 18,
    display: 'flex',
    flexDirection: 'column',
    justifyContent: 'space-between',
    minHeight: 120,
  } as const;

  const metricCardBodyStyle = {
    padding: 18,
    minHeight: 120,
    display: 'flex',
    flexDirection: 'column',
    justifyContent: 'space-between',
  } as const;

  const summaryCards = [
    {
      key: 'proxy',
      title: '代理',
      value: runningProxies,
      suffix: '/ 2',
      icon: <ApiOutlined />,
      color: '#1677ff',
      note: 'SOCKS5 / HTTP',
    },
    {
      key: 'tunnel',
      title: 'Tunnel',
      value: onlineTunnelClients,
      suffix: `/ ${tunnelClients.length}`,
      icon: <DeploymentUnitOutlined />,
      color: '#0f9f6e',
      note: `${enabledTunnelRoutes} 条路由已启用`,
    },
    {
      key: 'user',
      title: '用户',
      value: userCount,
      suffix: '',
      icon: <UserOutlined />,
      color: '#d97706',
      note: '管理访问账户',
    },
    {
      key: 'security',
      title: '白名单',
      value: whitelistCount,
      suffix: '',
      icon: <SafetyOutlined />,
      color: '#475569',
      note: '访问控制规则',
    },
  ] as const;

  return (
    <div>
      <Title level={3} style={{ marginBottom: 20 }}>
        <ThunderboltOutlined style={{ marginRight: 8, color: '#1890ff' }} />
        系统概览
      </Title>

      <Row gutter={[16, 16]}>
        {summaryCards.map((card) => (
          <Col key={card.key} xs={24} sm={12} lg={6}>
            <Card
              loading={loading}
              bordered={false}
              style={summaryCardStyle}
              styles={{ body: summaryCardBodyStyle }}
            >
              <Space direction="vertical" size={12} style={{ width: '100%' }}>
                <div style={{ display: 'flex', alignItems: 'center' }}>
                  <Space size={10}>
                    <div
                      style={{
                        width: 34,
                        height: 34,
                        borderRadius: 8,
                        background: `${card.color}12`,
                        color: card.color,
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'center',
                        fontSize: 16,
                        flex: '0 0 auto',
                      }}
                    >
                      {card.icon}
                    </div>
                    <div>
                      <Text style={{ display: 'block', color: '#0f172a', fontWeight: 600, lineHeight: 1.2 }}>{card.title}</Text>
                      <Text type="secondary" style={{ fontSize: 11, lineHeight: 1.3 }}>{card.note}</Text>
                    </div>
                  </Space>
                </div>

                <Statistic
                  value={card.value}
                  suffix={card.suffix}
                  valueStyle={{ color: '#0f172a', fontSize: 26, fontWeight: 700, lineHeight: 1 }}
                />
              </Space>
            </Card>
          </Col>
        ))}
      </Row>

      <div style={sectionTitleStyle}>
        <span>运行状态</span>
        <div style={sectionLineStyle} />
      </div>
      <Row gutter={[16, 16]}>
        <Col xs={24} lg={8}>
          <Card
            title="Tunnel"
            bordered={false}
            loading={loading}
            extra={statusTag(tunnelRunning)}
            style={{ ...panelCardStyle, height: '100%' }}
            styles={compactCardStyles}
          >
            <Descriptions
              size="small"
              column={1}
              items={[
                { key: 'listen', label: '地址', children: tunnelListenSummary || '-' },
                {
                  key: 'tls',
                  label: 'TLS',
                  children: tunnelStatus?.certificates.ready ? <Tag color="success">就绪</Tag> : <Tag>未就绪</Tag>,
                },
                { key: 'clients', label: '客户端', children: `${onlineTunnelClients} / ${tunnelClients.length}` },
                { key: 'routes', label: '路由', children: `${enabledTunnelRoutes} / ${tunnelRoutes.length}` },
              ]}
            />
          </Card>
        </Col>

        <Col xs={24} lg={16}>
          <Card
            title="代理服务"
            bordered={false}
            loading={loading}
            style={{ ...panelCardStyle, height: '100%' }}
            styles={compactCardStyles}
          >
            <Row gutter={[16, 16]}>
              <Col xs={24} md={12}>
                <div
                  style={{
                    padding: '12px 14px',
                    borderRadius: 10,
                    background: '#f8fafc',
                    border: '1px solid #edf2f7',
                    height: '100%',
                  }}
                >
                  <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 10 }}>
                    <Text strong>SOCKS5</Text>
                    {statusTag(proxyStatus?.socks5?.running)}
                  </div>
                  <Descriptions
                    size="small"
                    column={2}
                    items={[
                      { key: 'socks5-port', label: '端口', children: proxyStatus?.socks5?.port || '-' },
                      { key: 'socks5-bind', label: 'Bind', children: boolTag(proxyStatus?.socks5?.bindListen) },
                      { key: 'socks5-auto', label: '自启', children: boolTag(proxyStatus?.socks5?.autoStart) },
                    ]}
                  />
                </div>
              </Col>

              <Col xs={24} md={12}>
                <div
                  style={{
                    padding: '12px 14px',
                    borderRadius: 10,
                    background: '#f8fafc',
                    border: '1px solid #edf2f7',
                    height: '100%',
                  }}
                >
                  <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 10 }}>
                    <Text strong>HTTP</Text>
                    {statusTag(proxyStatus?.http?.running)}
                  </div>
                  <Descriptions
                    size="small"
                    column={2}
                    items={[
                      { key: 'http-port', label: '端口', children: proxyStatus?.http?.port || '-' },
                      { key: 'http-bind', label: 'Bind', children: boolTag(proxyStatus?.http?.bindListen) },
                      { key: 'http-auto', label: '自启', children: boolTag(proxyStatus?.http?.autoStart) },
                    ]}
                  />
                </div>
              </Col>
            </Row>
          </Card>
        </Col>
      </Row>

      <div style={sectionTitleStyle}>
        <span>实时指标</span>
        <div style={sectionLineStyle} />
      </div>
      <Row gutter={[16, 16]}>
        <Col xs={24} sm={12} lg={6}>
          <Card loading={loading} bordered={false} style={panelCardStyle} styles={{ body: metricCardBodyStyle }}>
            <Statistic
              title="活跃连接"
              value={metrics?.activeConnections || 0}
              prefix={<LinkOutlined />}
              valueStyle={{ color: '#1677ff', fontSize: 26 }}
            />
            <div style={{ marginTop: 12 }}>
              <Tag color="blue">峰值 {metrics?.maxActiveConnections || 0}</Tag>
            </div>
          </Card>
        </Col>

        <Col xs={24} sm={12} lg={6}>
          <Card loading={loading} bordered={false} style={panelCardStyle} styles={{ body: metricCardBodyStyle }}>
            <Statistic
              title="上传速度"
              value={metrics ? formatSpeed(metrics.uploadSpeed) : '0 B/s'}
              prefix={<CloudUploadOutlined />}
              valueStyle={{ color: '#0f9f6e', fontSize: 22 }}
            />
            <div style={{ marginTop: 12 }}>
              <Tag color="green">峰值 {metrics ? formatSpeed(metrics.maxUploadSpeed) : '0 B/s'}</Tag>
            </div>
          </Card>
        </Col>

        <Col xs={24} sm={12} lg={6}>
          <Card loading={loading} bordered={false} style={panelCardStyle} styles={{ body: metricCardBodyStyle }}>
            <Statistic
              title="下载速度"
              value={metrics ? formatSpeed(metrics.downloadSpeed) : '0 B/s'}
              prefix={<CloudDownloadOutlined />}
              valueStyle={{ color: '#0f62fe', fontSize: 22 }}
            />
            <div style={{ marginTop: 12 }}>
              <Tag color="cyan">峰值 {metrics ? formatSpeed(metrics.maxDownloadSpeed) : '0 B/s'}</Tag>
            </div>
          </Card>
        </Col>

        <Col xs={24} sm={12} lg={6}>
          <Card loading={loading} bordered={false} style={panelCardStyle} styles={{ body: metricCardBodyStyle }}>
            <Statistic
              title="错误"
              value={metrics?.errorCount || 0}
              prefix={<WarningOutlined />}
              valueStyle={{ color: (metrics?.errorCount || 0) > 0 ? '#ff4d4f' : '#52c41a', fontSize: 26 }}
            />
            <div style={{ marginTop: 12 }}>
              <Tag color={(metrics?.errorCount || 0) > 0 ? 'error' : 'success'}>
                运行 {metrics ? formatUptime(metrics.uptime) : '0分钟'}
              </Tag>
            </div>
          </Card>
        </Col>
      </Row>

      <div style={sectionTitleStyle}>
        <span>趋势</span>
        <div style={sectionLineStyle} />
      </div>
      <Row gutter={[16, 16]}>
        <Col xs={24} xl={14}>
          <Card
            title="流量趋势"
            extra={<Tag>最近 1 小时</Tag>}
            bordered={false}
            loading={loading}
            style={panelCardStyle}
            styles={compactCardStyles}
          >
            <Line {...bandwidthConfig} height={260} />
          </Card>
        </Col>

        <Col xs={24} xl={10}>
          <Card
            title="连接趋势"
            bordered={false}
            loading={loading}
            style={panelCardStyle}
            styles={compactCardStyles}
          >
            <Line {...connectionsConfig} height={260} />
          </Card>
        </Col>
      </Row>

      <div style={sectionTitleStyle}>
        <span>累计统计</span>
        <div style={sectionLineStyle} />
      </div>
      <Row gutter={[16, 16]}>
        <Col xs={24} sm={12} lg={6}>
          <Card loading={loading} bordered={false} style={panelCardStyle} styles={{ body: { padding: 18 } }}>
            <Statistic title="总连接" value={metrics?.totalConnections || 0} />
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card loading={loading} bordered={false} style={panelCardStyle} styles={{ body: { padding: 18 } }}>
            <Statistic title="接收流量" value={metrics ? formatBytes(metrics.bytesReceived) : '0 B'} />
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card loading={loading} bordered={false} style={panelCardStyle} styles={{ body: { padding: 18 } }}>
            <Statistic title="发送流量" value={metrics ? formatBytes(metrics.bytesSent) : '0 B'} />
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card loading={loading} bordered={false} style={panelCardStyle} styles={{ body: { padding: 18 } }}>
            <Statistic title="运行时长" value={metrics ? formatUptime(metrics.uptime) : '0分钟'} />
          </Card>
        </Col>
      </Row>
    </div>
  );
};

export default Dashboard;
