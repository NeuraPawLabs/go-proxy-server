import React, { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Button,
  Card,
  Descriptions,
  Empty,
  Form,
  Input,
  InputNumber,
  Modal,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  Tooltip,
  Typography,
  message,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import {
  ArrowLeftOutlined,
  DeleteOutlined,
  EditOutlined,
  PlusOutlined,
  QuestionCircleOutlined,
  ReloadOutlined,
} from '@ant-design/icons';
import {
  deleteTunnelRoute,
  getManagedTunnelSessions,
  getTunnelClients,
  getTunnelRoutes,
  saveTunnelRoute,
} from '../../api/tunnel';
import { getApiErrorMessage } from '../../api';
import type { ManagedTunnelSession, TunnelClient, TunnelProtocol, TunnelRoute } from '../../types/tunnel';
import { formatSessionAge, formatTransferSize } from '../../utils/tunnelSession';

const { Text } = Typography;

type BatchWhitelistMode = 'replace' | 'append' | 'clear';

interface TunnelClientDetailProps {
  clientName: string;
  onBack: () => void;
  onOpenClient: (clientName: string) => void;
}

interface RouteFormValues {
  name: string;
  protocol: TunnelProtocol;
  targetAddr: string;
  publicPort: number;
  ipWhitelist: string[];
  udpIdleTimeoutSec: number;
  udpMaxPayload: number;
  enabled: boolean;
}

interface BatchWhitelistFormValues {
  mode: BatchWhitelistMode;
  ipWhitelist: string[];
}

const defaultRouteFormValues: RouteFormValues = {
  name: '',
  protocol: 'tcp',
  targetAddr: '',
  publicPort: 0,
  ipWhitelist: [],
  udpIdleTimeoutSec: 60,
  udpMaxPayload: 1200,
  enabled: true,
};

const whitelistTemplates = [
  { key: 'open', label: '开放所有', values: [] as string[] },
  { key: 'loopback', label: '仅本机', values: ['127.0.0.1/32', '::1/128'] },
  {
    key: 'private',
    label: '常见内网',
    values: ['10.0.0.0/8', '172.16.0.0/12', '192.168.0.0/16'],
  },
] as const;

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

function mergeWhitelist(baseValues: readonly string[], extraValues: readonly string[]): string[] {
  return normalizeWhitelist([...baseValues, ...extraValues]);
}

const TunnelClientDetail: React.FC<TunnelClientDetailProps> = ({
  clientName,
  onBack,
  onOpenClient,
}) => {
  const [clients, setClients] = useState<TunnelClient[]>([]);
  const [routes, setRoutes] = useState<TunnelRoute[]>([]);
  const [sessions, setSessions] = useState<ManagedTunnelSession[]>([]);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [batchSaving, setBatchSaving] = useState(false);
  const [editingRouteKey, setEditingRouteKey] = useState<string | null>(null);
  const [routeModalOpen, setRouteModalOpen] = useState(false);
  const [selectedRouteIds, setSelectedRouteIds] = useState<React.Key[]>([]);
  const [batchEditVisible, setBatchEditVisible] = useState(false);
  const [messageApi, contextHolder] = message.useMessage();
  const [form] = Form.useForm<RouteFormValues>();
  const [batchForm] = Form.useForm<BatchWhitelistFormValues>();
  const routeProtocol = (Form.useWatch('protocol', form) ?? 'tcp') as TunnelProtocol;

  const loadData = useCallback(async () => {
    setLoading(true);
    try {
      const [clientsRes, routesRes, sessionsRes] = await Promise.all([
        getTunnelClients(),
        getTunnelRoutes(),
        getManagedTunnelSessions({ clientName }),
      ]);
      setClients(clientsRes.data || []);
      setRoutes(routesRes.data || []);
      setSessions(sessionsRes.data || []);
    } catch (error) {
      console.error('Failed to load tunnel client detail:', error);
      messageApi.error('加载客户端详情失败');
    } finally {
      setLoading(false);
    }
  }, [clientName, messageApi]);

  useEffect(() => {
    void loadData();
  }, [loadData]);

  const selectedClient = useMemo(
    () => clients.find((client) => client.name === clientName),
    [clientName, clients],
  );

  const clientRoutes = useMemo(
    () => routes.filter((route) => route.clientName === clientName),
    [clientName, routes],
  );

  const selectedRoutes = useMemo(
    () => clientRoutes.filter((route) => selectedRouteIds.includes(route.id)),
    [clientRoutes, selectedRouteIds],
  );

  const routeSessionCounts = useMemo(() => {
    const counts = new Map<string, number>();
    sessions.forEach((session) => {
      counts.set(session.routeName, (counts.get(session.routeName) || 0) + 1);
    });
    return counts;
  }, [sessions]);

  const closeRouteModal = useCallback(() => {
    setRouteModalOpen(false);
    setEditingRouteKey(null);
    form.resetFields();
    form.setFieldsValue(defaultRouteFormValues);
  }, [form]);

  const openCreateRouteModal = useCallback(() => {
    setEditingRouteKey(null);
    form.resetFields();
    form.setFieldsValue(defaultRouteFormValues);
    setRouteModalOpen(true);
  }, [form]);

  useEffect(() => {
    closeRouteModal();
    setSelectedRouteIds([]);
  }, [clientName, closeRouteModal]);

  useEffect(() => {
    if (selectedClient?.engine !== 'quic' && routeProtocol === 'udp') {
      form.setFieldValue('protocol', 'tcp');
    }
  }, [form, routeProtocol, selectedClient?.engine]);

  const handleSave = async (values: RouteFormValues) => {
    setSaving(true);
    try {
      await saveTunnelRoute({
        clientName,
        name: values.name.trim(),
        protocol: values.protocol,
        targetAddr: values.targetAddr.trim(),
        publicPort: values.publicPort ?? 0,
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
      messageApi.error(getApiErrorMessage(error, '保存路由失败'));
    } finally {
      setSaving(false);
    }
  };

  const handleEdit = (route: TunnelRoute) => {
    setEditingRouteKey(`${route.clientName}/${route.name}`);
    form.setFieldsValue({
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
  };

  const handleDelete = (route: TunnelRoute) => {
    Modal.confirm({
      title: '删除透传路由',
      content: `确认删除 ${route.clientName}/${route.name} 吗？`,
      okText: '删除',
      okType: 'danger',
      cancelText: '取消',
      onOk: async () => {
        try {
          await deleteTunnelRoute(route.clientName, route.name);
          messageApi.success('路由已删除');
          if (editingRouteKey === `${route.clientName}/${route.name}`) {
            closeRouteModal();
          }
          await loadData();
        } catch (error) {
          console.error('Failed to delete tunnel route:', error);
          messageApi.error(getApiErrorMessage(error, '删除路由失败'));
        }
      },
    });
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
      messageApi.error(getApiErrorMessage(error, '更新路由状态失败'));
    }
  };

  const applyWhitelistTemplateToForm = (values: readonly string[]) => {
    form.setFieldValue('ipWhitelist', [...values]);
  };

  const applyWhitelistTemplateToBatchForm = (values: readonly string[]) => {
    batchForm.setFieldValue('ipWhitelist', [...values]);
  };

  const openBatchWhitelistEditor = () => {
    batchForm.setFieldsValue({
      mode: 'replace',
      ipWhitelist: [],
    });
    setBatchEditVisible(true);
  };

  const handleBatchWhitelistSave = async (values: BatchWhitelistFormValues) => {
    if (selectedRoutes.length === 0) {
      messageApi.warning('请先选择要批量编辑的路由');
      return;
    }

    setBatchSaving(true);
    try {
      const normalizedInput = normalizeWhitelist(values.ipWhitelist || []);

      for (const route of selectedRoutes) {
        let nextWhitelist: string[] = [];
        switch (values.mode) {
          case 'clear':
            nextWhitelist = [];
            break;
          case 'append':
            nextWhitelist = mergeWhitelist(route.ipWhitelist || [], normalizedInput);
            break;
          default:
            nextWhitelist = normalizedInput;
            break;
        }

        await saveTunnelRoute({
          clientName: route.clientName,
          name: route.name,
          protocol: route.protocol,
          targetAddr: route.targetAddr,
          publicPort: route.publicPort,
          ipWhitelist: nextWhitelist,
          udpIdleTimeoutSec: route.udpIdleTimeoutSec,
          udpMaxPayload: route.udpMaxPayload,
          enabled: route.enabled,
        });
      }

      messageApi.success(`已批量更新 ${selectedRoutes.length} 条路由的白名单`);
      setBatchEditVisible(false);
      setSelectedRouteIds([]);
      await loadData();
    } catch (error) {
      console.error('Failed to batch update route whitelist:', error);
      messageApi.error(getApiErrorMessage(error, '批量更新白名单失败'));
    } finally {
      setBatchSaving(false);
    }
  };

  const routeColumns: ColumnsType<TunnelRoute> = [
    {
      title: '路由名称',
      key: 'name',
      render: (_, record) => (
        <Space direction="vertical" size={2}>
          <Space size={8}>
            <Text strong>{record.name}</Text>
            <Tag color={record.protocol === 'udp' ? 'purple' : 'blue'}>{record.protocol.toUpperCase()}</Tag>
          </Space>
          {record.protocol === 'udp' ? (
            <Text type="secondary">
              超时 {record.udpIdleTimeoutSec}s / 单包 {record.udpMaxPayload}B
            </Text>
          ) : null}
        </Space>
      ),
    },
    {
      title: '目标地址',
      dataIndex: 'targetAddr',
      key: 'targetAddr',
    },
    {
      title: '连接端口',
      key: 'ports',
      render: (_, record) => {
        const displayPort =
          record.activePublicPort ||
          (record.publicPort === 0 ? record.assignedPublicPort : record.publicPort);
        const strategyText =
          record.publicPort === 0
            ? `自动分配端口: ${record.assignedPublicPort || '待分配'}`
            : `固定端口: ${record.publicPort}`;
        return (
          <Space direction="vertical" size={4}>
            <Space size={6} wrap>
              <Text strong>{displayPort || '待分配'}</Text>
              {record.activePublicPort ? (
                <Tag color="green">当前生效</Tag>
              ) : record.publicPort === 0 && record.assignedPublicPort ? (
                <Tag>上次分配</Tag>
              ) : null}
            </Space>
            <Text type="secondary">{strategyText}</Text>
          </Space>
        );
      },
    },
    {
      title: '来源 IP 白名单',
      dataIndex: 'ipWhitelist',
      key: 'ipWhitelist',
      render: (values: string[]) => {
        if (!values || values.length === 0) {
          return <Text type="secondary">未限制</Text>;
        }
        return (
          <Space size={[4, 4]} wrap>
            {values.map((value) => (
              <Tag key={value}>{value}</Tag>
            ))}
          </Space>
        );
      },
    },
    {
      title: '实时连接',
      key: 'activeSessions',
      render: (_, record) => {
        const count = routeSessionCounts.get(record.name) || 0;
        return <Tag color={count > 0 ? 'green' : 'default'}>{count}</Tag>;
      },
    },
    {
      title: '启用',
      key: 'enabled',
      render: (_, record) => (
        <Switch checked={record.enabled} onChange={(checked) => void handleToggleRoute(record, checked)} />
      ),
    },
    {
      title: '最近错误',
      dataIndex: 'lastError',
      key: 'lastError',
      render: (value: string) => (value ? <Text type="danger">{value}</Text> : '-'),
    },
    {
      title: '操作',
      key: 'actions',
      render: (_, record) => (
        <Space>
          <Button icon={<EditOutlined />} onClick={() => handleEdit(record)}>
            编辑
          </Button>
          <Button danger icon={<DeleteOutlined />} onClick={() => handleDelete(record)}>
            删除
          </Button>
        </Space>
      ),
    },
  ];

  const sessionColumns: ColumnsType<ManagedTunnelSession> = [
    {
      title: '路由',
      key: 'routeName',
      render: (_, record) => (
        <Space direction="vertical" size={2}>
          <Space size={8}>
            <Text strong>{record.routeName}</Text>
            <Tag color={record.protocol === 'udp' ? 'purple' : 'blue'}>{record.protocol.toUpperCase()}</Tag>
            <Tag>{record.engine.toUpperCase()}</Tag>
          </Space>
          <Text type="secondary">{record.targetAddr}</Text>
        </Space>
      ),
    },
    {
      title: '来源地址',
      dataIndex: 'sourceAddr',
      key: 'sourceAddr',
      render: (value: string) => <Text code>{value}</Text>,
    },
    {
      title: '连接端口',
      dataIndex: 'publicPort',
      key: 'publicPort',
      render: (value: number) => <Text strong>{value || '-'}</Text>,
    },
    {
      title: '持续时长',
      dataIndex: 'openedAt',
      key: 'openedAt',
      render: (value: string) => formatSessionAge(value),
    },
    {
      title: '最近活动',
      dataIndex: 'lastActivityAt',
      key: 'lastActivityAt',
      render: (value: string) => (value ? new Date(value).toLocaleString() : '-'),
    },
    {
      title: '流量',
      key: 'traffic',
      render: (_, record) => (
        <Space direction="vertical" size={2}>
          <Text>入站 {formatTransferSize(record.bytesFromPublic)}</Text>
          <Text type="secondary">出站 {formatTransferSize(record.bytesToPublic)}</Text>
          {record.protocol === 'udp' ? (
            <Text type="secondary">
              包数 {record.packetsFromPublic} / {record.packetsToPublic}
            </Text>
          ) : null}
        </Space>
      ),
    },
  ];

  const panelCardStyle = {
    borderRadius: 14,
    border: '1px solid #e6ebf2',
    boxShadow: '0 6px 18px rgba(15, 23, 42, 0.04)',
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

  return (
    <Space direction="vertical" size="large" style={{ width: '100%' }}>
      {contextHolder}
      <Modal
        title={editingRouteKey ? '编辑路由' : '新建路由'}
        open={routeModalOpen}
        onCancel={closeRouteModal}
        onOk={() => {
          void form.submit();
        }}
        confirmLoading={saving}
        okText={editingRouteKey ? '保存修改' : '创建路由'}
        cancelText="取消"
        width={620}
        destroyOnClose={false}
      >
        <Form<RouteFormValues>
          form={form}
          layout="vertical"
          initialValues={defaultRouteFormValues}
          onFinish={(values) => {
            void handleSave(values);
          }}
        >
          <Form.Item
            label="路由名称"
            name="name"
            rules={[{ required: true, message: '请输入路由名称' }]}
          >
            <Input placeholder="mysql-prod" />
          </Form.Item>
          <Form.Item
            label={labelWithHint('转发协议', 'Classic 仅支持 TCP；QUIC 客户端支持 TCP 和 UDP')}
            name="protocol"
            rules={[{ required: true, message: '请选择转发协议' }]}
          >
            <Select<TunnelProtocol>
              options={[
                { value: 'tcp', label: 'TCP' },
                {
                  value: 'udp',
                  label: 'UDP',
                  disabled: selectedClient?.engine !== 'quic',
                },
              ]}
            />
          </Form.Item>
          <Form.Item
            label={labelWithHint(
              '目标地址',
              routeProtocol === 'udp'
                ? '支持 host:port，也支持直接填 53 或 :53'
                : '支持 host:port，也支持直接填 3306 或 :3306',
            )}
            name="targetAddr"
            rules={[{ required: true, message: '请输入目标地址' }]}
          >
            <Input placeholder={routeProtocol === 'udp' ? '127.0.0.1:53 或 53' : '127.0.0.1:3306 或 3306'} />
          </Form.Item>
          {routeProtocol === 'udp' ? (
            <>
              <Form.Item
                label={labelWithHint('UDP 空闲超时', '超过该时长无报文往来时，自动回收 UDP 会话')}
                name="udpIdleTimeoutSec"
                rules={[{ required: true, message: '请输入 UDP 空闲超时' }]}
              >
                <InputNumber min={5} max={3600} style={{ width: '100%' }} addonAfter="秒" />
              </Form.Item>
              <Form.Item
                label={labelWithHint('UDP 单包大小', '限制每个 UDP 报文的最大负载，建议保持 1200 左右')}
                name="udpMaxPayload"
                rules={[{ required: true, message: '请输入 UDP 单包大小' }]}
              >
                <InputNumber min={256} max={65507} style={{ width: '100%' }} addonAfter="bytes" />
              </Form.Item>
            </>
          ) : null}
          <Form.Item
            label={labelWithHint('公网端口', '填 0 表示按内网穿透服务配置自动分配')}
            name="publicPort"
          >
            <InputNumber min={0} max={65535} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item
            label={labelWithHint('来源 IP', '支持 IP 或 CIDR')}
            name="ipWhitelist"
          >
            <Select
              mode="tags"
              tokenSeparators={[',']}
              placeholder="输入后回车"
              open={false}
            />
          </Form.Item>
          <Form.Item label={labelWithHint('白名单模板', '点击后覆盖当前列表')}>
            <Space wrap>
              {whitelistTemplates.map((template) => (
                <Button
                  key={template.key}
                  size="small"
                  onClick={() => applyWhitelistTemplateToForm(template.values)}
                >
                  {template.label}
                </Button>
              ))}
            </Space>
          </Form.Item>
          <Form.Item label="启用状态" name="enabled" valuePropName="checked">
            <Switch checkedChildren="启用" unCheckedChildren="停用" />
          </Form.Item>
        </Form>
      </Modal>
      <Modal
        title={`批量编辑白名单 (${selectedRoutes.length} 条路由)`}
        open={batchEditVisible}
        onCancel={() => setBatchEditVisible(false)}
        onOk={() => {
          void batchForm.submit();
        }}
        confirmLoading={batchSaving}
        okText="保存批量修改"
        cancelText="取消"
      >
        <Form<BatchWhitelistFormValues>
          form={batchForm}
          layout="vertical"
          initialValues={{ mode: 'replace', ipWhitelist: [] }}
          onFinish={(values) => {
            void handleBatchWhitelistSave(values);
          }}
        >
          <Form.Item label="批量操作方式" name="mode">
            <Select<BatchWhitelistMode>
              options={[
                { value: 'replace', label: '覆盖为以下白名单' },
                { value: 'append', label: '追加以下白名单' },
                { value: 'clear', label: '清空白名单限制' },
              ]}
            />
          </Form.Item>
          <Form.Item noStyle shouldUpdate>
            {() => {
              const mode = batchForm.getFieldValue('mode') as BatchWhitelistMode;
              if (mode === 'clear') {
                return <Text type="warning">将清空选中路由的白名单</Text>;
              }

              return (
                <>
                  <Form.Item label={labelWithHint('白名单模板', '点击后填充到下方列表')}>
                    <Space wrap>
                      {whitelistTemplates.map((template) => (
                        <Button
                          key={template.key}
                          size="small"
                          onClick={() => applyWhitelistTemplateToBatchForm(template.values)}
                        >
                          {template.label}
                        </Button>
                      ))}
                    </Space>
                  </Form.Item>
                  <Form.Item
                    label={labelWithHint('来源 IP', '支持 IP 或 CIDR')}
                    name="ipWhitelist"
                  >
                    <Select
                      mode="tags"
                      tokenSeparators={[',']}
                      placeholder="输入后回车"
                      open={false}
                    />
                  </Form.Item>
                </>
              );
            }}
          </Form.Item>
        </Form>
      </Modal>
      <Card bordered={false} style={panelCardStyle} styles={{ body: { padding: 18 } }}>
        <Space style={{ width: '100%', justifyContent: 'space-between' }} wrap>
          <Space wrap>
            <Button icon={<ArrowLeftOutlined />} onClick={onBack}>
              返回
            </Button>
            <Select
              value={clientName}
              style={{ minWidth: 240 }}
              options={clients.map((client) => ({ value: client.name, label: client.name }))}
              onChange={onOpenClient}
              placeholder="切换客户端"
              showSearch
              optionFilterProp="label"
            />
          </Space>
          <Button icon={<ReloadOutlined />} onClick={() => void loadData()} loading={loading}>
            刷新
          </Button>
        </Space>
      </Card>

      {!selectedClient ? (
        <Card bordered={false} style={panelCardStyle} styles={{ body: { padding: 18 } }}>
          <Empty description="客户端不存在" />
        </Card>
      ) : (
        <>
          <div style={sectionTitleStyle}>
            <span>客户端状态</span>
            <div style={sectionLineStyle} />
          </div>
          <Card title="客户端" bordered={false} style={panelCardStyle} styles={compactCardStyles}>
            <Descriptions column={{ xs: 1, sm: 2, xl: 3 }} bordered size="small">
              <Descriptions.Item label="客户端名称">{selectedClient.name}</Descriptions.Item>
              <Descriptions.Item label="连接状态">
                <Tag color={getClientStatusMeta(selectedClient).color}>
                  {getClientStatusMeta(selectedClient).label}
                </Tag>
              </Descriptions.Item>
              <Descriptions.Item label="远端地址">
                {selectedClient.remoteAddr || '-'}
              </Descriptions.Item>
              <Descriptions.Item label="隧道引擎">
                <Tag color={selectedClient.engine === 'quic' ? 'cyan' : 'blue'}>
                  {selectedClient.engine.toUpperCase()}
                </Tag>
              </Descriptions.Item>
              <Descriptions.Item label="最后心跳">
                {selectedClient.lastSeenAt ? new Date(selectedClient.lastSeenAt).toLocaleString() : '-'}
              </Descriptions.Item>
              <Descriptions.Item label="已配置路由">
                {selectedClient.routeCount}
              </Descriptions.Item>
              <Descriptions.Item label="已生效路由">
                {selectedClient.activeRouteCount}
              </Descriptions.Item>
            </Descriptions>
          </Card>

          <div style={sectionTitleStyle}>
            <span>实时连接</span>
            <div style={sectionLineStyle} />
          </div>
          <Card
            title={`活跃会话 (${sessions.length})`}
            bordered={false}
            style={panelCardStyle}
            styles={compactCardStyles}
          >
            {sessions.length > 0 ? (
              <Table<ManagedTunnelSession>
                rowKey="id"
                loading={loading}
                columns={sessionColumns}
                dataSource={sessions}
                pagination={sessions.length > 8 ? { pageSize: 8 } : false}
                locale={{ emptyText: '当前客户端没有活跃连接' }}
              />
            ) : (
              <Empty description="当前客户端没有活跃连接" />
            )}
          </Card>

          <div style={sectionTitleStyle}>
            <span>路由管理</span>
            <div style={sectionLineStyle} />
          </div>
          <Card
            title={`路由 (${clientRoutes.length})`}
            extra={(
              <Space wrap>
                <Button icon={<PlusOutlined />} type="primary" onClick={openCreateRouteModal}>
                  新建路由
                </Button>
                <Button
                  onClick={() => setSelectedRouteIds([])}
                  disabled={selectedRouteIds.length === 0}
                >
                  清空选择
                </Button>
                <Button
                  onClick={openBatchWhitelistEditor}
                  disabled={selectedRouteIds.length === 0}
                >
                  批量白名单
                </Button>
              </Space>
            )}
            bordered={false}
            style={panelCardStyle}
            styles={compactCardStyles}
          >
            <Table<TunnelRoute>
              rowKey="id"
              loading={loading}
              columns={routeColumns}
              dataSource={clientRoutes}
              rowSelection={{
                selectedRowKeys: selectedRouteIds,
                onChange: setSelectedRouteIds,
              }}
              pagination={clientRoutes.length > 10 ? { pageSize: 10 } : false}
              locale={{ emptyText: '当前客户端暂无路由' }}
            />
          </Card>
        </>
      )}
    </Space>
  );
};

export default TunnelClientDetail;
