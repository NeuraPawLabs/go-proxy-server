import React, { useState, useEffect } from 'react';
import { Row, Col, Typography } from 'antd';
import { ControlOutlined } from '@ant-design/icons';
import ProxyCard from './ProxyCard';
import { getProxyStatus } from '../../api/proxy';
import type { ProxyStatus } from '../../types/proxy';

const { Title } = Typography;

const ProxyControl: React.FC = () => {
  const [status, setStatus] = useState<ProxyStatus | null>(null);
  const [loading, setLoading] = useState(false);

  const loadStatus = async () => {
    try {
      setLoading(true);
      const response = await getProxyStatus();
      setStatus(response.data);
    } catch (error) {
      console.error('Failed to load proxy status:', error);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadStatus();
  }, []);

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

  return (
    <div>
      <Title level={3} style={{ marginBottom: 24 }}>
        <ControlOutlined style={{ marginRight: 8, color: '#1890ff' }} />
        代理控制
      </Title>

      <div style={sectionTitleStyle}>
        <span>代理服务</span>
        <div style={sectionLineStyle} />
      </div>
      <Row gutter={[24, 24]}>
        <Col xs={24} xl={12}>
          <ProxyCard
            type="socks5"
            title="SOCKS5 代理"
            status={status?.socks5}
            onStatusChange={loadStatus}
            loading={loading}
          />
        </Col>
        <Col xs={24} xl={12}>
          <ProxyCard
            type="http"
            title="HTTP 代理"
            status={status?.http}
            onStatusChange={loadStatus}
            loading={loading}
          />
        </Col>
      </Row>
    </div>
  );
};

export default ProxyControl;
