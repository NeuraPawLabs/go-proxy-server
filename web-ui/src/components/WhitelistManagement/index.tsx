import React, { useState, useEffect } from 'react';
import { Card, Button, Space, message, Typography, Row, Col, Modal } from 'antd';
import { ReloadOutlined, PlusOutlined, SafetyOutlined } from '@ant-design/icons';
import WhitelistTable from './WhitelistTable';
import AddIPForm from './AddIPForm';
import { getWhitelist, addWhitelistIP, deleteWhitelistIP } from '../../api/whitelist';

const { Title } = Typography;

const WhitelistManagement: React.FC = () => {
  const [whitelist, setWhitelist] = useState<string[]>([]);
  const [loading, setLoading] = useState(false);
  const [modalVisible, setModalVisible] = useState(false);

  const loadWhitelist = async () => {
    try {
      setLoading(true);
      const response = await getWhitelist();
      setWhitelist(response.data);
    } catch (error) {
      console.error('Failed to load whitelist:', error);
      message.error('加载白名单失败');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadWhitelist();
  }, []);

  const handleAddIP = async (ip: string) => {
    try {
      await addWhitelistIP(ip);
      message.success('IP 添加成功');
      setModalVisible(false);
      loadWhitelist();
    } catch (error) {
      console.error('Failed to add IP:', error);
      message.error('IP 添加失败');
    }
  };

  const handleDeleteIP = async (ip: string) => {
    try {
      await deleteWhitelistIP(ip);
      message.success('IP 删除成功');
      loadWhitelist();
    } catch (error) {
      console.error('Failed to delete IP:', error);
      message.error('IP 删除失败');
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
        <SafetyOutlined style={{ marginRight: 8, color: '#1890ff' }} />
        IP 白名单管理
      </Title>

      <Row gutter={[24, 24]}>
        <Col span={24}>
          <div style={sectionTitleStyle}>
            <span>访问控制</span>
            <div style={sectionLineStyle} />
          </div>
          <Card
            title={
              <Space>
                <SafetyOutlined style={{ fontSize: '18px', color: '#52c41a' }} />
                <span style={{ fontSize: '16px', fontWeight: 600 }}>IP 白名单列表 ({whitelist.length})</span>
              </Space>
            }
            bordered={false}
            style={panelCardStyle}
            styles={compactCardStyles}
            extra={
              <Space>
                <Button icon={<ReloadOutlined />} onClick={loadWhitelist} loading={loading}>
                  刷新
                </Button>
                <Button type="primary" icon={<PlusOutlined />} onClick={() => setModalVisible(true)}>
                  添加 IP
                </Button>
              </Space>
            }
          >
            <WhitelistTable
              whitelist={whitelist}
              loading={loading}
              onDelete={handleDeleteIP}
            />
          </Card>
        </Col>
      </Row>

      <Modal
        title={
          <Space>
            <PlusOutlined style={{ color: '#1890ff' }} />
            <span>添加 IP 白名单</span>
          </Space>
        }
        open={modalVisible}
        onCancel={() => setModalVisible(false)}
        footer={null}
        width={600}
      >
        <AddIPForm onSubmit={handleAddIP} />
      </Modal>
    </div>
  );
};

export default WhitelistManagement;
