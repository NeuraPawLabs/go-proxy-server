import React, { useState, useEffect } from 'react';
import { Card, Form, Switch, Button, Alert, message, Typography, Space, Row, Col } from 'antd';
import { SettingOutlined, SaveOutlined, WindowsOutlined, CheckCircleOutlined, WarningOutlined } from '@ant-design/icons';
import { getSystemSettings, saveSystemSettings } from '../../api/system';
import type { SystemSettings as SystemSettingsType } from '../../types/api';

const { Title, Text, Paragraph } = Typography;

const SystemSettings: React.FC = () => {
  const [form] = Form.useForm();
  const [loading, setLoading] = useState(false);
  const [settings, setSettings] = useState<SystemSettingsType | null>(null);

  const loadSettings = async () => {
    try {
      setLoading(true);
      const response = await getSystemSettings();
      setSettings(response.data);
      form.setFieldsValue({
        autostartEnabled: response.data.autostartEnabled,
      });
    } catch (error) {
      console.error('Failed to load system settings:', error);
      message.error('加载系统设置失败');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadSettings();
  }, []);

  const handleSave = async (values: { autostartEnabled: boolean }) => {
    try {
      setLoading(true);
      await saveSystemSettings(values.autostartEnabled);
      message.success('系统设置保存成功');
      loadSettings();
    } catch (error) {
      console.error('Failed to save system settings:', error);
      message.error('系统设置保存失败');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div>
      <Title level={3} style={{ marginBottom: 24 }}>
        <SettingOutlined style={{ marginRight: 8, color: '#1890ff' }} />
        系统设置
      </Title>

      <Row gutter={[24, 24]}>
        <Col span={24}>
          <Card
            bordered={false}
            style={{ boxShadow: '0 2px 8px rgba(0,0,0,0.1)' }}
            loading={loading}
          >
            {!settings?.autostartSupported && (
              <Alert
                message="平台限制"
                description="开机自启功能仅在 Windows 平台可用，当前平台不支持此功能。"
                type="info"
                showIcon
                icon={<WindowsOutlined />}
                style={{ marginBottom: 24 }}
              />
            )}

            <div style={{ marginBottom: 24 }}>
              <Space direction="vertical" size="small">
                <Text strong style={{ fontSize: '16px' }}>
                  <WindowsOutlined style={{ marginRight: 8, color: '#1890ff' }} />
                  Windows 开机自启
                </Text>
                <Paragraph type="secondary" style={{ marginBottom: 0 }}>
                  启用后，应用程序将在 Windows 系统启动时自动运行。此功能会修改 Windows 注册表中的启动项。
                </Paragraph>
              </Space>
            </div>

            <Form
              form={form}
              layout="vertical"
              onFinish={handleSave}
              initialValues={{ autostartEnabled: false }}
            >
              <div style={{
                padding: '20px',
                background: '#fafafa',
                borderRadius: '8px',
                display: 'flex',
                justifyContent: 'space-between',
                alignItems: 'center',
              }}>
                <Space>
                  <SettingOutlined style={{ fontSize: '24px', color: settings?.autostartSupported ? '#1890ff' : '#d9d9d9' }} />
                  <div>
                    <Text strong style={{ fontSize: '16px' }}>开机自启动</Text>
                    <br />
                    <Text type="secondary" style={{ fontSize: '13px' }}>
                      {settings?.autostartSupported ? '系统启动时自动运行应用' : '当前平台不支持'}
                    </Text>
                  </div>
                </Space>
                <Form.Item
                  name="autostartEnabled"
                  valuePropName="checked"
                  style={{ marginBottom: 0 }}
                >
                  <Switch
                    disabled={!settings?.autostartSupported}
                    checkedChildren="已启用"
                    unCheckedChildren="未启用"
                    size="default"
                  />
                </Form.Item>
              </div>

              {settings?.registryEnabled !== undefined && (
                <Alert
                  message="注册表状态"
                  description={
                    <Space>
                      {settings.registryEnabled ? (
                        <>
                          <CheckCircleOutlined style={{ color: '#52c41a' }} />
                          <Text>Windows 注册表启动项已配置</Text>
                        </>
                      ) : (
                        <>
                          <WarningOutlined style={{ color: '#faad14' }} />
                          <Text>Windows 注册表启动项未配置</Text>
                        </>
                      )}
                    </Space>
                  }
                  type={settings.registryEnabled ? 'success' : 'warning'}
                  showIcon
                  style={{ marginBottom: 24 }}
                />
              )}

              <Form.Item style={{ marginBottom: 0 }}>
                <Button
                  type="primary"
                  htmlType="submit"
                  loading={loading}
                  icon={<SaveOutlined />}
                  size="large"
                  disabled={!settings?.autostartSupported}
                >
                  保存设置
                </Button>
              </Form.Item>
            </Form>
          </Card>
        </Col>
      </Row>
    </div>
  );
};

export default SystemSettings;
