import React, { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Button,
  Card,
  Col,
  Empty,
  Input,
  Modal,
  Row,
  Select,
  Space,
  Statistic,
  Table,
  Tabs,
  Tag,
  Typography,
  message,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import {
  ClearOutlined,
  FileSearchOutlined,
  ReloadOutlined,
  SearchOutlined,
} from '@ant-design/icons';
import { getApiErrorMessage } from '../../api';
import { getAuditLogs, getEventLogs } from '../../api/logs';
import type {
  AuditLogFilters,
  AuditLogItem,
  EventLogFilters,
  EventLogItem,
  LogListResponse,
} from '../../types/logs';

const { Paragraph, Text, Title } = Typography;

type ActivityTabKey = 'audit' | 'events';

interface AuditDraftFilters {
  search: string;
  action?: string;
  status?: string;
  targetType?: string;
  from: string;
  to: string;
}

interface EventDraftFilters {
  search: string;
  category?: string;
  severity?: string;
  source?: string;
  eventType?: string;
  from: string;
  to: string;
}

interface LogDetailState {
  title: string;
  items: Array<{ label: string; value: string }>;
  details?: Record<string, unknown>;
}

const DEFAULT_PAGE_SIZE = 20;

const emptyAuditResponse: LogListResponse<AuditLogItem> = {
  items: [],
  total: 0,
  page: 1,
  limit: DEFAULT_PAGE_SIZE,
  pages: 0,
  hasMore: false,
};

const emptyEventResponse: LogListResponse<EventLogItem> = {
  items: [],
  total: 0,
  page: 1,
  limit: DEFAULT_PAGE_SIZE,
  pages: 0,
  hasMore: false,
};

const defaultAuditDraft: AuditDraftFilters = {
  search: '',
  action: undefined,
  status: undefined,
  targetType: undefined,
  from: '',
  to: '',
};

const defaultEventDraft: EventDraftFilters = {
  search: '',
  category: undefined,
  severity: undefined,
  source: undefined,
  eventType: undefined,
  from: '',
  to: '',
};

function toRFC3339(value: string): string | undefined {
  if (value.trim() === '') {
    return undefined;
  }

  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return undefined;
  }

  return parsed.toISOString();
}

function formatDateTime(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }

  return new Intl.DateTimeFormat('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  }).format(date);
}

function formatInlineJSON(details?: Record<string, unknown>): string {
  if (!details || Object.keys(details).length === 0) {
    return '';
  }
  return JSON.stringify(details, null, 2);
}

function buildAuditQuery(draft: AuditDraftFilters, current: AuditLogFilters): AuditLogFilters {
  return {
    page: 1,
    limit: current.limit ?? DEFAULT_PAGE_SIZE,
    action: draft.action,
    status: draft.status,
    targetType: draft.targetType,
    search: draft.search.trim() || undefined,
    from: toRFC3339(draft.from),
    to: toRFC3339(draft.to),
  };
}

function buildEventQuery(draft: EventDraftFilters, current: EventLogFilters): EventLogFilters {
  return {
    page: 1,
    limit: current.limit ?? DEFAULT_PAGE_SIZE,
    category: draft.category,
    severity: draft.severity,
    source: draft.source,
    eventType: draft.eventType,
    search: draft.search.trim() || undefined,
    from: toRFC3339(draft.from),
    to: toRFC3339(draft.to),
  };
}

function countActiveFilters(values: Array<string | undefined>): number {
  return values.filter((value) => typeof value === 'string' && value.trim() !== '').length;
}

function getAuditStatusTag(status: string): React.ReactNode {
  switch (status) {
    case 'success':
      return <Tag color="success">成功</Tag>;
    case 'failure':
      return <Tag color="error">失败</Tag>;
    default:
      return <Tag>{status || '未知'}</Tag>;
  }
}

function getEventSeverityTag(severity: string): React.ReactNode {
  switch (severity) {
    case 'error':
      return <Tag color="error">错误</Tag>;
    case 'warn':
      return <Tag color="warning">告警</Tag>;
    case 'info':
      return <Tag color="processing">信息</Tag>;
    default:
      return <Tag>{severity || '未知'}</Tag>;
  }
}

const ActivityLogs: React.FC = () => {
  const [activeTab, setActiveTab] = useState<ActivityTabKey>('audit');
  const [auditDraft, setAuditDraft] = useState<AuditDraftFilters>(defaultAuditDraft);
  const [eventDraft, setEventDraft] = useState<EventDraftFilters>(defaultEventDraft);
  const [auditQuery, setAuditQuery] = useState<AuditLogFilters>({ page: 1, limit: DEFAULT_PAGE_SIZE });
  const [eventQuery, setEventQuery] = useState<EventLogFilters>({ page: 1, limit: DEFAULT_PAGE_SIZE });
  const [auditData, setAuditData] = useState<LogListResponse<AuditLogItem>>(emptyAuditResponse);
  const [eventData, setEventData] = useState<LogListResponse<EventLogItem>>(emptyEventResponse);
  const [auditLoading, setAuditLoading] = useState(false);
  const [eventLoading, setEventLoading] = useState(false);
  const [detailState, setDetailState] = useState<LogDetailState | null>(null);
  const [messageApi, contextHolder] = message.useMessage();

  const loadAuditData = useCallback(async (filters: AuditLogFilters) => {
    setAuditLoading(true);
    try {
      const response = await getAuditLogs(filters);
      setAuditData(response);
    } catch (error) {
      console.error('Failed to load audit logs:', error);
      messageApi.error(getApiErrorMessage(error, '加载审计日志失败'));
    } finally {
      setAuditLoading(false);
    }
  }, [messageApi]);

  const panelCardStyle = {
    borderRadius: 14,
    border: '1px solid #e6ebf2',
    boxShadow: '0 6px 18px rgba(15, 23, 42, 0.04)',
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

  const loadEventData = useCallback(async (filters: EventLogFilters) => {
    setEventLoading(true);
    try {
      const response = await getEventLogs(filters);
      setEventData(response);
    } catch (error) {
      console.error('Failed to load event logs:', error);
      messageApi.error(getApiErrorMessage(error, '加载事件日志失败'));
    } finally {
      setEventLoading(false);
    }
  }, [messageApi]);

  useEffect(() => {
    void loadAuditData(auditQuery);
  }, [auditQuery, loadAuditData]);

  useEffect(() => {
    void loadEventData(eventQuery);
  }, [eventQuery, loadEventData]);

  const auditSummary = useMemo(() => {
    const successCount = auditData.items.filter((item) => item.status === 'success').length;
    return {
      successCount,
      failureCount: auditData.items.length - successCount,
      filterCount: countActiveFilters([
        auditDraft.search,
        auditDraft.action,
        auditDraft.status,
        auditDraft.targetType,
        auditDraft.from,
        auditDraft.to,
      ]),
    };
  }, [auditData.items, auditDraft]);

  const eventSummary = useMemo(() => {
    const warnCount = eventData.items.filter((item) => item.severity === 'warn').length;
    const errorCount = eventData.items.filter((item) => item.severity === 'error').length;
    return {
      warnCount,
      errorCount,
      filterCount: countActiveFilters([
        eventDraft.search,
        eventDraft.category,
        eventDraft.severity,
        eventDraft.source,
        eventDraft.eventType,
        eventDraft.from,
        eventDraft.to,
      ]),
    };
  }, [eventData.items, eventDraft]);

  const auditActionOptions = useMemo(() => {
    const values = new Set<string>([
      'admin.bootstrap',
      'admin.login',
      'admin.logout',
      'user.create',
      'user.delete',
      'whitelist.add',
      'whitelist.delete',
      'proxy.start',
      'proxy.stop',
      'proxy.config.update',
      'system.config.update',
      'system.shutdown',
      'tunnel.server.start',
      'tunnel.server.stop',
      'tunnel.certificates.upload',
      'tunnel.certificates.generate',
      'tunnel.client_ca.download',
      'tunnel.route.upsert',
      'tunnel.route.delete',
    ]);
    auditData.items.forEach((item) => values.add(item.action));
    return Array.from(values).sort().map((value) => ({ label: value, value }));
  }, [auditData.items]);

  const auditTargetOptions = useMemo(() => {
    const values = new Set<string>(['admin', 'user', 'whitelist', 'proxy', 'system', 'tunnel_server', 'tunnel_route']);
    auditData.items.forEach((item) => values.add(item.targetType));
    return Array.from(values).sort().map((value) => ({ label: value, value }));
  }, [auditData.items]);

  const eventCategoryOptions = useMemo(() => {
    const values = new Set<string>(['auth', 'security', 'proxy', 'tunnel']);
    eventData.items.forEach((item) => values.add(item.category));
    return Array.from(values).sort().map((value) => ({ label: value, value }));
  }, [eventData.items]);

  const eventSourceOptions = useMemo(() => {
    const values = new Set<string>(['web_admin', 'http_proxy', 'socks5_proxy', 'tunnel_server', 'tunnel_client']);
    eventData.items.forEach((item) => values.add(item.source));
    return Array.from(values).sort().map((value) => ({ label: value, value }));
  }, [eventData.items]);

  const eventTypeOptions = useMemo(() => {
    const values = new Set<string>([
      'admin_auth_required',
      'admin_login_failed',
      'admin_login_succeeded',
      'captcha_verification_failed',
      'captcha_verification_error',
      'managed_client_connected',
      'managed_client_disconnected',
      'managed_route_exposed',
      'managed_route_expose_failed',
      'legacy_tunnel_exposed',
      'legacy_tunnel_stopped',
      'route_unavailable',
      'target_connect_failed',
      'data_connection_rejected',
      'ssrf_blocked',
      'dns_rebinding_blocked',
      'rate_limit_blocked',
      'proxy_auth_failed',
      'unauthorized_attempt',
      'dns_resolution_failed',
    ]);
    eventData.items.forEach((item) => values.add(item.eventType));
    return Array.from(values).sort().map((value) => ({ label: value, value }));
  }, [eventData.items]);

  const openAuditDetail = useCallback((item: AuditLogItem) => {
    setDetailState({
      title: `审计日志 #${item.id}`,
      items: [
        { label: '发生时间', value: formatDateTime(item.occurredAt) },
        { label: '动作', value: item.action },
        { label: '执行主体', value: `${item.actorType}:${item.actorId}` },
        { label: '操作目标', value: `${item.targetType}:${item.targetId || '-'}` },
        { label: '状态', value: item.status },
        { label: '来源 IP', value: item.sourceIp || '-' },
        { label: 'User-Agent', value: item.userAgent || '-' },
        { label: '说明', value: item.message || '-' },
      ],
      details: item.details,
    });
  }, []);

  const openEventDetail = useCallback((item: EventLogItem) => {
    setDetailState({
      title: `事件日志 #${item.id}`,
      items: [
        { label: '发生时间', value: formatDateTime(item.occurredAt) },
        { label: '分类', value: item.category },
        { label: '事件类型', value: item.eventType },
        { label: '严重级别', value: item.severity },
        { label: '来源', value: item.source },
        { label: '说明', value: item.message || '-' },
      ],
      details: item.details,
    });
  }, []);

  const auditColumns = useMemo<ColumnsType<AuditLogItem>>(() => [
    {
      title: '时间',
      dataIndex: 'occurredAt',
      key: 'occurredAt',
      width: 180,
      render: (value: string) => <Text>{formatDateTime(value)}</Text>,
    },
    {
      title: '动作',
      dataIndex: 'action',
      key: 'action',
      width: 180,
      render: (value: string) => <Text code>{value}</Text>,
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 100,
      render: (value: string) => getAuditStatusTag(value),
    },
    {
      title: '执行主体',
      key: 'actor',
      width: 160,
      render: (_, item) => <Text>{`${item.actorType}:${item.actorId}`}</Text>,
    },
    {
      title: '操作目标',
      key: 'target',
      width: 180,
      render: (_, item) => <Text>{`${item.targetType}:${item.targetId || '-'}`}</Text>,
    },
    {
      title: '来源 IP',
      dataIndex: 'sourceIp',
      key: 'sourceIp',
      width: 150,
      render: (value: string) => <Text>{value || '-'}</Text>,
    },
    {
      title: '说明',
      dataIndex: 'message',
      key: 'message',
      render: (value: string) => (
        <Paragraph ellipsis={{ rows: 2, tooltip: value || '-' }} style={{ marginBottom: 0 }}>
          {value || '-'}
        </Paragraph>
      ),
    },
    {
      title: '详情',
      key: 'details',
      width: 90,
      fixed: 'right',
      render: (_, item) => (
        <Button size="small" onClick={() => openAuditDetail(item)}>
          查看
        </Button>
      ),
    },
  ], [openAuditDetail]);

  const eventColumns = useMemo<ColumnsType<EventLogItem>>(() => [
    {
      title: '时间',
      dataIndex: 'occurredAt',
      key: 'occurredAt',
      width: 180,
      render: (value: string) => <Text>{formatDateTime(value)}</Text>,
    },
    {
      title: '级别',
      dataIndex: 'severity',
      key: 'severity',
      width: 100,
      render: (value: string) => getEventSeverityTag(value),
    },
    {
      title: '分类',
      dataIndex: 'category',
      key: 'category',
      width: 120,
      render: (value: string) => <Text code>{value}</Text>,
    },
    {
      title: '事件类型',
      dataIndex: 'eventType',
      key: 'eventType',
      width: 220,
      render: (value: string) => <Text code>{value}</Text>,
    },
    {
      title: '来源',
      dataIndex: 'source',
      key: 'source',
      width: 160,
      render: (value: string) => <Text>{value}</Text>,
    },
    {
      title: '说明',
      dataIndex: 'message',
      key: 'message',
      render: (value: string) => (
        <Paragraph ellipsis={{ rows: 2, tooltip: value || '-' }} style={{ marginBottom: 0 }}>
          {value || '-'}
        </Paragraph>
      ),
    },
    {
      title: '详情',
      key: 'details',
      width: 90,
      fixed: 'right',
      render: (_, item) => (
        <Button size="small" onClick={() => openEventDetail(item)}>
          查看
        </Button>
      ),
    },
  ], [openEventDetail]);

  const auditFilterCard = (
    <Card
      bordered={false}
      style={{ ...panelCardStyle, marginBottom: 20 }}
      title="审计"
      styles={compactCardStyles}
      extra={
        <Space>
          <Tag color="blue">已启用筛选 {auditSummary.filterCount}</Tag>
          <Button
            icon={<ReloadOutlined />}
            onClick={() => void loadAuditData(auditQuery)}
            loading={auditLoading}
          >
            刷新
          </Button>
        </Space>
      }
    >
      <Row gutter={[16, 16]}>
        <Col xs={24} md={12} xl={8}>
          <Input
            allowClear
            prefix={<SearchOutlined />}
            placeholder="搜索动作 / 主体 / 目标"
            value={auditDraft.search}
            onChange={(event) => setAuditDraft((current) => ({ ...current, search: event.target.value }))}
          />
        </Col>
        <Col xs={24} sm={12} xl={4}>
          <Select
            allowClear
            placeholder="动作"
            options={auditActionOptions}
            style={{ width: '100%' }}
            value={auditDraft.action}
            onChange={(value) => setAuditDraft((current) => ({ ...current, action: value }))}
          />
        </Col>
        <Col xs={24} sm={12} xl={4}>
          <Select
            allowClear
            placeholder="状态"
            options={[
              { label: '成功', value: 'success' },
              { label: '失败', value: 'failure' },
            ]}
            style={{ width: '100%' }}
            value={auditDraft.status}
            onChange={(value) => setAuditDraft((current) => ({ ...current, status: value }))}
          />
        </Col>
        <Col xs={24} sm={12} xl={4}>
          <Select
            allowClear
            placeholder="目标类型"
            options={auditTargetOptions}
            style={{ width: '100%' }}
            value={auditDraft.targetType}
            onChange={(value) => setAuditDraft((current) => ({ ...current, targetType: value }))}
          />
        </Col>
        <Col xs={24} sm={12} xl={4}>
          <Input
            type="datetime-local"
            value={auditDraft.from}
            onChange={(event) => setAuditDraft((current) => ({ ...current, from: event.target.value }))}
          />
        </Col>
        <Col xs={24} sm={12} xl={4}>
          <Input
            type="datetime-local"
            value={auditDraft.to}
            onChange={(event) => setAuditDraft((current) => ({ ...current, to: event.target.value }))}
          />
        </Col>
        <Col span={24}>
          <Space wrap>
            <Button
              type="primary"
              icon={<SearchOutlined />}
              onClick={() => setAuditQuery((current) => buildAuditQuery(auditDraft, current))}
            >
              查询
            </Button>
            <Button
              icon={<ClearOutlined />}
              onClick={() => {
                setAuditDraft(defaultAuditDraft);
                setAuditQuery((current) => ({ page: 1, limit: current.limit ?? DEFAULT_PAGE_SIZE }));
              }}
            >
              重置
            </Button>
          </Space>
        </Col>
      </Row>
    </Card>
  );

  const eventFilterCard = (
    <Card
      bordered={false}
      style={{ ...panelCardStyle, marginBottom: 20 }}
      title="事件"
      styles={compactCardStyles}
      extra={
        <Space>
          <Tag color="blue">已启用筛选 {eventSummary.filterCount}</Tag>
          <Button
            icon={<ReloadOutlined />}
            onClick={() => void loadEventData(eventQuery)}
            loading={eventLoading}
          >
            刷新
          </Button>
        </Space>
      }
    >
      <Row gutter={[16, 16]}>
        <Col xs={24} md={12} xl={8}>
          <Input
            allowClear
            prefix={<SearchOutlined />}
            placeholder="搜索说明 / 来源 / 类型"
            value={eventDraft.search}
            onChange={(event) => setEventDraft((current) => ({ ...current, search: event.target.value }))}
          />
        </Col>
        <Col xs={24} sm={12} xl={4}>
          <Select
            allowClear
            placeholder="分类"
            options={eventCategoryOptions}
            style={{ width: '100%' }}
            value={eventDraft.category}
            onChange={(value) => setEventDraft((current) => ({ ...current, category: value }))}
          />
        </Col>
        <Col xs={24} sm={12} xl={4}>
          <Select
            allowClear
            placeholder="级别"
            options={[
              { label: '信息', value: 'info' },
              { label: '告警', value: 'warn' },
              { label: '错误', value: 'error' },
            ]}
            style={{ width: '100%' }}
            value={eventDraft.severity}
            onChange={(value) => setEventDraft((current) => ({ ...current, severity: value }))}
          />
        </Col>
        <Col xs={24} sm={12} xl={4}>
          <Select
            allowClear
            placeholder="来源"
            options={eventSourceOptions}
            style={{ width: '100%' }}
            value={eventDraft.source}
            onChange={(value) => setEventDraft((current) => ({ ...current, source: value }))}
          />
        </Col>
        <Col xs={24} sm={12} xl={4}>
          <Select
            allowClear
            placeholder="事件类型"
            options={eventTypeOptions}
            style={{ width: '100%' }}
            value={eventDraft.eventType}
            onChange={(value) => setEventDraft((current) => ({ ...current, eventType: value }))}
          />
        </Col>
        <Col xs={24} sm={12} xl={4}>
          <Input
            type="datetime-local"
            value={eventDraft.from}
            onChange={(event) => setEventDraft((current) => ({ ...current, from: event.target.value }))}
          />
        </Col>
        <Col xs={24} sm={12} xl={4}>
          <Input
            type="datetime-local"
            value={eventDraft.to}
            onChange={(event) => setEventDraft((current) => ({ ...current, to: event.target.value }))}
          />
        </Col>
        <Col span={24}>
          <Space wrap>
            <Button
              type="primary"
              icon={<SearchOutlined />}
              onClick={() => setEventQuery((current) => buildEventQuery(eventDraft, current))}
            >
              查询
            </Button>
            <Button
              icon={<ClearOutlined />}
              onClick={() => {
                setEventDraft(defaultEventDraft);
                setEventQuery((current) => ({ page: 1, limit: current.limit ?? DEFAULT_PAGE_SIZE }));
              }}
            >
              重置
            </Button>
          </Space>
        </Col>
      </Row>
    </Card>
  );

  return (
    <div>
      {contextHolder}
      <Space direction="vertical" size={20} style={{ width: '100%' }}>
        <div>
          <Title level={3} style={{ marginBottom: 0 }}>
            <FileSearchOutlined style={{ marginRight: 8, color: '#1677ff' }} />
            日志中心
          </Title>
        </div>

        <Tabs
          activeKey={activeTab}
          onChange={(key) => setActiveTab(key as ActivityTabKey)}
          items={[
            {
              key: 'audit',
              label: '审计日志',
              children: (
                <Space direction="vertical" size={20} style={{ width: '100%' }}>
                  <Row gutter={[16, 16]}>
                    <Col xs={24} md={12} xl={6}>
                      <Card bordered={false} style={panelCardStyle} styles={{ body: { padding: 18 } }}>
                        <Statistic title="审计总量" value={auditData.total} />
                      </Card>
                    </Col>
                    <Col xs={24} md={12} xl={6}>
                      <Card bordered={false} style={panelCardStyle} styles={{ body: { padding: 18 } }}>
                        <Statistic title="当前页记录" value={auditData.items.length} />
                      </Card>
                    </Col>
                    <Col xs={24} md={12} xl={6}>
                      <Card bordered={false} style={panelCardStyle} styles={{ body: { padding: 18 } }}>
                        <Statistic title="本页成功" value={auditSummary.successCount} valueStyle={{ color: '#389e0d' }} />
                      </Card>
                    </Col>
                    <Col xs={24} md={12} xl={6}>
                      <Card bordered={false} style={panelCardStyle} styles={{ body: { padding: 18 } }}>
                        <Statistic title="本页失败" value={auditSummary.failureCount} valueStyle={{ color: '#cf1322' }} />
                      </Card>
                    </Col>
                  </Row>
                  {auditFilterCard}
                  <Card bordered={false} style={panelCardStyle} styles={{ body: { padding: 18 } }}>
                    <Table<AuditLogItem>
                      rowKey="id"
                      columns={auditColumns}
                      dataSource={auditData.items}
                      loading={auditLoading}
                      locale={{
                        emptyText: <Empty description="暂无符合条件的审计日志" />,
                      }}
                      pagination={{
                        current: auditData.page,
                        pageSize: auditData.limit,
                        total: auditData.total,
                        showSizeChanger: true,
                        showTotal: (total) => `共 ${total} 条`,
                        onChange: (page, pageSize) => {
                          setAuditQuery((current) => ({ ...current, page, limit: pageSize }));
                        },
                      }}
                      scroll={{ x: 1320 }}
                    />
                  </Card>
                </Space>
              ),
            },
            {
              key: 'events',
              label: '事件日志',
              children: (
                <Space direction="vertical" size={20} style={{ width: '100%' }}>
                  <Row gutter={[16, 16]}>
                    <Col xs={24} md={12} xl={6}>
                      <Card bordered={false} style={panelCardStyle} styles={{ body: { padding: 18 } }}>
                        <Statistic title="事件总量" value={eventData.total} />
                      </Card>
                    </Col>
                    <Col xs={24} md={12} xl={6}>
                      <Card bordered={false} style={panelCardStyle} styles={{ body: { padding: 18 } }}>
                        <Statistic title="当前页记录" value={eventData.items.length} />
                      </Card>
                    </Col>
                    <Col xs={24} md={12} xl={6}>
                      <Card bordered={false} style={panelCardStyle} styles={{ body: { padding: 18 } }}>
                        <Statistic title="本页告警" value={eventSummary.warnCount} valueStyle={{ color: '#d48806' }} />
                      </Card>
                    </Col>
                    <Col xs={24} md={12} xl={6}>
                      <Card bordered={false} style={panelCardStyle} styles={{ body: { padding: 18 } }}>
                        <Statistic title="本页错误" value={eventSummary.errorCount} valueStyle={{ color: '#cf1322' }} />
                      </Card>
                    </Col>
                  </Row>
                  {eventFilterCard}
                  <Card bordered={false} style={panelCardStyle} styles={{ body: { padding: 18 } }}>
                    <Table<EventLogItem>
                      rowKey="id"
                      columns={eventColumns}
                      dataSource={eventData.items}
                      loading={eventLoading}
                      locale={{
                        emptyText: <Empty description="暂无符合条件的事件日志" />,
                      }}
                      pagination={{
                        current: eventData.page,
                        pageSize: eventData.limit,
                        total: eventData.total,
                        showSizeChanger: true,
                        showTotal: (total) => `共 ${total} 条`,
                        onChange: (page, pageSize) => {
                          setEventQuery((current) => ({ ...current, page, limit: pageSize }));
                        },
                      }}
                      scroll={{ x: 1240 }}
                    />
                  </Card>
                </Space>
              ),
            },
          ]}
        />
      </Space>

      <Modal
        open={detailState !== null}
        title={detailState?.title}
        onCancel={() => setDetailState(null)}
        footer={null}
        width={760}
      >
        {detailState ? (
          <Space direction="vertical" size={16} style={{ width: '100%' }}>
            <div
              style={{
                display: 'grid',
                gridTemplateColumns: 'minmax(120px, 180px) 1fr',
                gap: '12px 16px',
                alignItems: 'start',
              }}
            >
              {detailState.items.map((entry) => (
                <React.Fragment key={entry.label}>
                  <Text type="secondary">{entry.label}</Text>
                  <Text>{entry.value || '-'}</Text>
                </React.Fragment>
              ))}
            </div>
            <div>
              <Text strong>结构化详情</Text>
              {detailState.details && Object.keys(detailState.details).length > 0 ? (
                <pre
                  style={{
                    marginTop: 12,
                    marginBottom: 0,
                    padding: 16,
                    background: '#0f172a',
                    color: '#e2e8f0',
                    borderRadius: 12,
                    overflowX: 'auto',
                    fontSize: 13,
                    lineHeight: 1.6,
                  }}
                >
                  {formatInlineJSON(detailState.details)}
                </pre>
              ) : (
                <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="该日志没有额外结构化字段" />
              )}
            </div>
          </Space>
        ) : null}
      </Modal>
    </div>
  );
};

export default ActivityLogs;
