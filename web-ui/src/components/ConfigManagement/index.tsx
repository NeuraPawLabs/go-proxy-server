import React, { useCallback, useEffect, useState } from 'react';
import { Card, Form, InputNumber, Button, Row, Col, message, Typography, Switch, Space, Tooltip, Tag } from 'antd';
import { SettingOutlined, SaveOutlined, ClockCircleOutlined, ApiOutlined, WindowsOutlined, SafetyOutlined, QuestionCircleOutlined } from '@ant-design/icons';
import { getConfig, saveConfig } from '../../api/config';
import type { UnifiedConfig } from '../../types/api';

const { Title, Text } = Typography;

interface ConfigFormValues {
  connect: number;
  idleRead: number;
  idleWrite: number;
  maxConcurrentConnections: number;
  maxConcurrentConnectionsPerIP: number;
  autostartEnabled: boolean;
  allowPrivateIPAccess: boolean;
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

const ConfigManagement: React.FC = () => {
  const [form] = Form.useForm<ConfigFormValues>();
  const [loading, setLoading] = useState(false);
  const [config, setConfig] = useState<UnifiedConfig | null>(null);
  const showWindowsStartupSettings = config !== null && config.system.platform === 'windows';

  const loadConfig = useCallback(async () => {
    try {
      setLoading(true);
      const response = await getConfig();
      setConfig(response.data);
      form.setFieldsValue({
        connect: response.data.timeout.connect,
        idleRead: response.data.timeout.idleRead,
        idleWrite: response.data.timeout.idleWrite,
        maxConcurrentConnections: response.data.limiter.maxConcurrentConnections,
        maxConcurrentConnectionsPerIP: response.data.limiter.maxConcurrentConnectionsPerIP,
        autostartEnabled: response.data.system.autostartEnabled,
        allowPrivateIPAccess: response.data.security.allowPrivateIPAccess,
      });
    } catch (error) {
      console.error('Failed to load config:', error);
      message.error('加载配置失败');
    } finally {
      setLoading(false);
    }
  }, [form]);

  useEffect(() => {
    void loadConfig();
  }, [loadConfig]);

  const handleSave = async (values: ConfigFormValues) => {
    try {
      setLoading(true);
      await saveConfig({
        timeout: {
          connect: values.connect,
          idleRead: values.idleRead,
          idleWrite: values.idleWrite,
        },
        limiter: {
          maxConcurrentConnections: values.maxConcurrentConnections,
          maxConcurrentConnectionsPerIP: values.maxConcurrentConnectionsPerIP,
        },
        system: showWindowsStartupSettings ? {
          autostartEnabled: values.autostartEnabled,
        } : undefined,
        security: {
          allowPrivateIPAccess: values.allowPrivateIPAccess,
        },
      });
      message.success('配置保存成功');
      void loadConfig();
    } catch (error) {
      console.error('Failed to save config:', error);
      message.error('配置保存失败');
    } finally {
      setLoading(false);
    }
  };

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
    <div>
      <Title level={3} style={{ marginBottom: 24 }}>
        <SettingOutlined style={{ marginRight: 8, color: '#1890ff' }} />
        系统配置
      </Title>

      <Form
        form={form}
        layout="vertical"
        onFinish={handleSave}
        initialValues={{
          connect: 30,
          idleRead: 300,
          idleWrite: 300,
          maxConcurrentConnections: 100000,
          maxConcurrentConnectionsPerIP: 1000,
          autostartEnabled: false,
          allowPrivateIPAccess: false,
        }}
      >
        <Row gutter={[24, 24]}>
          <Col span={24}>
            <div style={sectionTitleStyle}>
              <span>基础参数</span>
              <div style={sectionLineStyle} />
            </div>
          </Col>
          <Col span={24}>
            <Card
              title={
                <Space>
                  <ClockCircleOutlined style={{ color: '#1890ff' }} />
                  <Text strong>超时</Text>
                </Space>
              }
              bordered={false}
              style={panelCardStyle}
              loading={loading}
              styles={compactCardStyles}
            >
              <Row gutter={24}>
                <Col xs={24} md={8}>
                  <Form.Item
                    label={labelWithHint('连接（秒）', '建立新连接的最长等待时间')}
                    name="connect"
                    rules={[
                      { required: true, message: '请输入连接超时时间' },
                      { type: 'number', min: 1, max: 300, message: '范围: 1-300 秒' },
                    ]}
                  >
                    <InputNumber min={1} max={300} style={{ width: '100%' }} size="large" />
                  </Form.Item>
                </Col>

                <Col xs={24} md={8}>
                  <Form.Item
                    label={labelWithHint('读空闲（秒）', '空闲连接等待读取数据的最长时间')}
                    name="idleRead"
                    rules={[
                      { required: true, message: '请输入空闲读取超时时间' },
                      { type: 'number', min: 1, max: 3600, message: '范围: 1-3600 秒' },
                    ]}
                  >
                    <InputNumber min={1} max={3600} style={{ width: '100%' }} size="large" />
                  </Form.Item>
                </Col>

                <Col xs={24} md={8}>
                  <Form.Item
                    label={labelWithHint('写空闲（秒）', '空闲连接等待写入数据的最长时间')}
                    name="idleWrite"
                    rules={[
                      { required: true, message: '请输入空闲写入超时时间' },
                      { type: 'number', min: 1, max: 3600, message: '范围: 1-3600 秒' },
                    ]}
                  >
                    <InputNumber min={1} max={3600} style={{ width: '100%' }} size="large" />
                  </Form.Item>
                </Col>
              </Row>
            </Card>
          </Col>

          <Col span={24}>
            <Card
              title={
                <Space>
                  <ApiOutlined style={{ color: '#1890ff' }} />
                  <Text strong>连接</Text>
                </Space>
              }
              bordered={false}
              style={panelCardStyle}
              loading={loading}
              styles={compactCardStyles}
            >
              <Row gutter={24}>
                <Col xs={24} md={12}>
                  <Form.Item
                    label={labelWithHint('总并发', '系统允许的最大并发连接数')}
                    name="maxConcurrentConnections"
                    rules={[
                      { required: true, message: '请输入最大并发连接数' },
                      { type: 'number', min: 1, max: 1000000, message: '范围: 1-1000000' },
                    ]}
                  >
                    <InputNumber min={1} max={1000000} style={{ width: '100%' }} size="large" />
                  </Form.Item>
                </Col>

                <Col xs={24} md={12}>
                  <Form.Item
                    label={labelWithHint('单 IP 并发', '单个来源 IP 可占用的最大并发数')}
                    name="maxConcurrentConnectionsPerIP"
                    rules={[
                      { required: true, message: '请输入单IP最大并发连接数' },
                      { type: 'number', min: 1, max: 100000, message: '范围: 1-100000' },
                    ]}
                  >
                    <InputNumber min={1} max={100000} style={{ width: '100%' }} size="large" />
                  </Form.Item>
                </Col>
              </Row>
            </Card>
          </Col>

          <Col span={24}>
            <div style={sectionTitleStyle}>
              <span>安全与系统</span>
              <div style={sectionLineStyle} />
            </div>
          </Col>
          <Col span={24}>
            <Card
              title={
                <Space>
                  <SafetyOutlined style={{ color: '#1890ff' }} />
                  <Text strong>安全</Text>
                </Space>
              }
              bordered={false}
              style={panelCardStyle}
              loading={loading}
              styles={compactCardStyles}
            >
              <div
                style={{
                  padding: '16px 20px',
                  background: '#fafafa',
                  borderRadius: '8px',
                  display: 'flex',
                  justifyContent: 'space-between',
                  alignItems: 'center',
                }}
              >
                <Space size={8}>{labelWithHint('允许内网地址', '开启后放宽 SSRF 保护')}</Space>
                <Form.Item
                  name="allowPrivateIPAccess"
                  valuePropName="checked"
                  style={{ marginBottom: 0 }}
                >
                  <Switch checkedChildren="开" unCheckedChildren="关" size="default" />
                </Form.Item>
              </div>
            </Card>
          </Col>

          {showWindowsStartupSettings && (
            <Col span={24}>
              <Card
                title={
                  <Space>
                    <WindowsOutlined style={{ color: '#1890ff' }} />
                    <Text strong>Windows</Text>
                  </Space>
                }
                bordered={false}
                style={panelCardStyle}
                loading={loading}
                styles={compactCardStyles}
              >
                <div
                  style={{
                    padding: '16px 20px',
                    background: '#fafafa',
                    borderRadius: '8px',
                    display: 'flex',
                    justifyContent: 'space-between',
                    alignItems: 'center',
                  }}
                >
                  <Space size={8}>
                    {labelWithHint('开机自启', '随系统启动应用')}
                    {!config?.system.autostartSupported ? <Tag>不可用</Tag> : null}
                  </Space>
                  <Form.Item
                    name="autostartEnabled"
                    valuePropName="checked"
                    style={{ marginBottom: 0 }}
                  >
                    <Switch
                      disabled={!config?.system.autostartSupported}
                      checkedChildren="开"
                      unCheckedChildren="关"
                      size="default"
                    />
                  </Form.Item>
                </div>

                {config?.system.registryEnabled !== undefined && (
                  <div style={{ marginTop: 12 }}>
                    <Tag color={config.system.registryEnabled ? 'success' : 'warning'}>
                      {config.system.registryEnabled ? '注册表已配置' : '注册表未配置'}
                    </Tag>
                  </div>
                )}
              </Card>
            </Col>
          )}

          <Col span={24}>
            <Card bordered={false} style={panelCardStyle} styles={{ body: { padding: 18 } }}>
              <Form.Item style={{ marginBottom: 0 }}>
                <Button
                  type="primary"
                  htmlType="submit"
                  loading={loading}
                  icon={<SaveOutlined />}
                  size="large"
                  block
                >
                  保存配置
                </Button>
              </Form.Item>
            </Card>
          </Col>
        </Row>
      </Form>
    </div>
  );
};

export default ConfigManagement;
