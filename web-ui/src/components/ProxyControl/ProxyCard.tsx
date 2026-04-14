import React, { useState, useEffect } from 'react';
import { Card, Button, InputNumber, Switch, Space, Form, message, Badge, Typography, Tooltip } from 'antd';
import { PlayCircleOutlined, StopOutlined, SaveOutlined, ApiOutlined, QuestionCircleOutlined } from '@ant-design/icons';
import { startProxy, stopProxy, saveProxyConfig } from '../../api/proxy';
import type { ProxyServerStatus } from '../../types/proxy';

const { Text } = Typography;

interface ProxyCardProps {
  type: 'socks5' | 'http';
  title: string;
  status?: ProxyServerStatus;
  onStatusChange: () => void;
  loading: boolean;
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

const ProxyCard: React.FC<ProxyCardProps> = ({ type, title, status, onStatusChange, loading }) => {
  const [form] = Form.useForm();
  const [actionLoading, setActionLoading] = useState(false);

  useEffect(() => {
    if (status) {
      form.setFieldsValue({
        port: status.port,
        bindListen: status.bindListen,
        autoStart: status.autoStart,
      });
    }
  }, [status, form]);

  const handleStart = async () => {
    try {
      const values = form.getFieldsValue();
      setActionLoading(true);

      // Save configuration first (including autoStart setting)
      await saveProxyConfig({
        type,
        port: values.port,
        bindListen: values.bindListen,
        autoStart: values.autoStart,
      });

      // Then start the proxy
      await startProxy({
        type,
        port: values.port,
        bindListen: values.bindListen,
      });
      message.success(`${title}启动成功`);
      onStatusChange();
    } catch (error) {
      console.error('Failed to start proxy:', error);
      message.error(`${title}启动失败`);
    } finally {
      setActionLoading(false);
    }
  };

  const handleStop = async () => {
    try {
      setActionLoading(true);
      await stopProxy({ type });
      message.success(`${title}停止成功`);
      onStatusChange();
    } catch (error) {
      console.error('Failed to stop proxy:', error);
      message.error(`${title}停止失败`);
    } finally {
      setActionLoading(false);
    }
  };

  const handleSaveConfig = async () => {
    try {
      const values = form.getFieldsValue();
      setActionLoading(true);
      await saveProxyConfig({
        type,
        port: values.port,
        bindListen: values.bindListen,
        autoStart: values.autoStart,
      });
      message.success('配置保存成功');
      onStatusChange();
    } catch (error) {
      console.error('Failed to save config:', error);
      message.error('配置保存失败');
    } finally {
      setActionLoading(false);
    }
  };

  const defaultPort = type === 'socks5' ? 1080 : 8080;
  const cardColor = type === 'socks5' ? '#1890ff' : '#722ed1';
  const panelCardStyle = {
    borderRadius: 14,
    border: '1px solid #e6ebf2',
    boxShadow: '0 6px 18px rgba(15, 23, 42, 0.04)',
  } as const;

  return (
    <Card
      loading={loading}
      bordered={false}
      style={panelCardStyle}
      styles={{ body: { padding: 18 } }}
    >
      <div style={{ marginBottom: 18 }}>
        <Space align="center" size="middle">
          <div style={{
            width: 42,
            height: 42,
            borderRadius: '10px',
            background: `${cardColor}12`,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
          }}>
            <ApiOutlined style={{ fontSize: '20px', color: cardColor }} />
          </div>
          <div>
            <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              <Text strong style={{ fontSize: '17px' }}>{title}</Text>
              {status?.running ? (
                <Badge status="processing" text="运行中" />
              ) : (
                <Badge status="default" text="已停止" />
              )}
            </div>
            <Text type="secondary" style={{ fontSize: '12px' }}>
              {type === 'socks5' ? 'SOCKS5 协议代理服务' : 'HTTP/HTTPS 协议代理服务'}
            </Text>
          </div>
        </Space>
      </div>

      <Form
        form={form}
        layout="vertical"
        initialValues={{ port: defaultPort, bindListen: false, autoStart: false }}
      >
        <Form.Item
          label={labelWithHint('监听端口', '代理服务监听的端口号')}
          name="port"
        >
          <InputNumber
            min={1}
            max={65535}
            style={{ width: '100%' }}
            disabled={status?.running}
            size="large"
            placeholder={`默认端口: ${defaultPort}`}
          />
        </Form.Item>

        <Form.Item
          label={labelWithHint('Bind-Listen', '使用客户端本地 IP 作为出站连接源地址')}
          name="bindListen"
          valuePropName="checked"
        >
          <Switch
            disabled={status?.running}
            checkedChildren="开"
            unCheckedChildren="关"
          />
        </Form.Item>

        <Form.Item
          label={labelWithHint('开机自启', '应用启动时自动启动此代理服务')}
          name="autoStart"
          valuePropName="checked"
        >
          <Switch
            checkedChildren="开"
            unCheckedChildren="关"
          />
        </Form.Item>

        <Space size="middle" style={{ width: '100%', justifyContent: 'flex-end' }}>
          <Button
            icon={<SaveOutlined />}
            onClick={handleSaveConfig}
            loading={actionLoading}
            size="large"
          >
            保存配置
          </Button>
          {status?.running ? (
            <Button
              type="primary"
              danger
              icon={<StopOutlined />}
              onClick={handleStop}
              loading={actionLoading}
              size="large"
            >
              停止服务
            </Button>
          ) : (
            <Button
              type="primary"
              icon={<PlayCircleOutlined />}
              onClick={handleStart}
              loading={actionLoading}
              size="large"
              style={{ background: cardColor, borderColor: cardColor }}
            >
              启动服务
            </Button>
          )}
        </Space>
      </Form>
    </Card>
  );
};

export default ProxyCard;
