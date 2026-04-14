import React, { useState, useEffect } from 'react';
import { Card, Form, InputNumber, Button, Row, Col, message, Typography, Alert } from 'antd';
import { ClockCircleOutlined, SaveOutlined } from '@ant-design/icons';
import { getTimeout, saveTimeout } from '../../api/timeout';
import type { TimeoutConfig as TimeoutConfigType } from '../../types/api';

const { Title, Text, Paragraph } = Typography;

const TimeoutConfig: React.FC = () => {
  const [form] = Form.useForm();
  const [loading, setLoading] = useState(false);

  const loadTimeout = async () => {
    try {
      setLoading(true);
      const response = await getTimeout();
      form.setFieldsValue(response.data);
    } catch (error) {
      console.error('Failed to load timeout config:', error);
      message.error('加载超时配置失败');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadTimeout();
  }, []);

  const handleSave = async (values: TimeoutConfigType) => {
    try {
      setLoading(true);
      await saveTimeout(values);
      message.success('超时配置保存成功，立即生效');
      loadTimeout();
    } catch (error) {
      console.error('Failed to save timeout config:', error);
      message.error('超时配置保存失败');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div>
      <Title level={3} style={{ marginBottom: 24 }}>
        <ClockCircleOutlined style={{ marginRight: 8, color: '#1890ff' }} />
        超时配置
      </Title>

      <Row gutter={[24, 24]}>
        <Col span={24}>
          <Card
            bordered={false}
            style={{ boxShadow: '0 2px 8px rgba(0,0,0,0.1)' }}
            loading={loading}
          >
            <Alert
              message="配置说明"
              description={
                <div>
                  <Paragraph style={{ marginBottom: 8 }}>
                    <Text strong>连接超时：</Text>建立连接时的最大等待时间，建议范围 10-60 秒
                  </Paragraph>
                  <Paragraph style={{ marginBottom: 8 }}>
                    <Text strong>空闲读取超时：</Text>连接空闲时等待读取数据的最大时间，建议范围 60-600 秒
                  </Paragraph>
                  <Paragraph style={{ marginBottom: 0 }}>
                    <Text strong>空闲写入超时：</Text>连接空闲时等待写入数据的最大时间，建议范围 60-600 秒
                  </Paragraph>
                </div>
              }
              type="info"
              showIcon
              style={{ marginBottom: 24 }}
            />

            <Form
              form={form}
              layout="vertical"
              onFinish={handleSave}
              initialValues={{ connect: 30, idleRead: 300, idleWrite: 300 }}
            >
              <Row gutter={24}>
                <Col xs={24} md={8}>
                  <Form.Item
                    label={<Text strong>连接超时（秒）</Text>}
                    name="connect"
                    rules={[
                      { required: true, message: '请输入连接超时时间' },
                      { type: 'number', min: 1, max: 300, message: '范围: 1-300 秒' },
                    ]}
                    tooltip="建立新连接时的最大等待时间"
                  >
                    <InputNumber
                      min={1}
                      max={300}
                      style={{ width: '100%' }}
                      placeholder="推荐: 30"
                      size="large"
                    />
                  </Form.Item>
                </Col>

                <Col xs={24} md={8}>
                  <Form.Item
                    label={<Text strong>空闲读取超时（秒）</Text>}
                    name="idleRead"
                    rules={[
                      { required: true, message: '请输入空闲读取超时时间' },
                      { type: 'number', min: 1, max: 3600, message: '范围: 1-3600 秒' },
                    ]}
                    tooltip="连接空闲时等待读取数据的最大时间"
                  >
                    <InputNumber
                      min={1}
                      max={3600}
                      style={{ width: '100%' }}
                      placeholder="推荐: 300"
                      size="large"
                    />
                  </Form.Item>
                </Col>

                <Col xs={24} md={8}>
                  <Form.Item
                    label={<Text strong>空闲写入超时（秒）</Text>}
                    name="idleWrite"
                    rules={[
                      { required: true, message: '请输入空闲写入超时时间' },
                      { type: 'number', min: 1, max: 3600, message: '范围: 1-3600 秒' },
                    ]}
                    tooltip="连接空闲时等待写入数据的最大时间"
                  >
                    <InputNumber
                      min={1}
                      max={3600}
                      style={{ width: '100%' }}
                      placeholder="推荐: 300"
                      size="large"
                    />
                  </Form.Item>
                </Col>
              </Row>

              <Form.Item style={{ marginTop: 24, marginBottom: 0 }}>
                <Button
                  type="primary"
                  htmlType="submit"
                  loading={loading}
                  icon={<SaveOutlined />}
                  size="large"
                >
                  保存配置
                </Button>
              </Form.Item>
            </Form>
          </Card>
        </Col>
      </Row>
    </div>
  );
};

export default TimeoutConfig;
