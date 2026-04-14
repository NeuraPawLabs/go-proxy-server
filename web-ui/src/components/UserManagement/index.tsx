import React, { useState, useEffect } from 'react';
import { Card, Button, Space, message, Typography, Row, Col, Modal } from 'antd';
import { ReloadOutlined, UserAddOutlined, TeamOutlined } from '@ant-design/icons';
import UserTable from './UserTable';
import AddUserForm from './AddUserForm';
import { getUsers, addUser, deleteUser } from '../../api/user';
import type { User, AddUserRequest } from '../../types/user';

const { Title } = Typography;

const UserManagement: React.FC = () => {
  const [users, setUsers] = useState<User[]>([]);
  const [loading, setLoading] = useState(false);
  const [modalVisible, setModalVisible] = useState(false);

  const loadUsers = async () => {
    try {
      setLoading(true);
      const response = await getUsers();
      setUsers(response.data);
    } catch (error) {
      console.error('Failed to load users:', error);
      message.error('加载用户列表失败');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadUsers();
  }, []);

  const handleAddUser = async (values: AddUserRequest) => {
    try {
      await addUser(values);
      message.success('用户添加成功');
      setModalVisible(false);
      loadUsers();
    } catch (error) {
      console.error('Failed to add user:', error);
      message.error('用户添加失败');
    }
  };

  const handleDeleteUser = async (username: string) => {
    try {
      await deleteUser({ username });
      message.success('用户删除成功');
      loadUsers();
    } catch (error) {
      console.error('Failed to delete user:', error);
      message.error('用户删除失败');
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
        <TeamOutlined style={{ marginRight: 8, color: '#1890ff' }} />
        用户管理
      </Title>

      <Row gutter={[24, 24]}>
        <Col span={24}>
          <div style={sectionTitleStyle}>
            <span>账户管理</span>
            <div style={sectionLineStyle} />
          </div>
          <Card
            title={
              <Space>
                <TeamOutlined style={{ fontSize: '18px', color: '#52c41a' }} />
                <span style={{ fontSize: '16px', fontWeight: 600 }}>用户列表 ({users.length})</span>
              </Space>
            }
            bordered={false}
            style={panelCardStyle}
            styles={compactCardStyles}
            extra={
              <Space>
                <Button icon={<ReloadOutlined />} onClick={loadUsers} loading={loading}>
                  刷新
                </Button>
                <Button type="primary" icon={<UserAddOutlined />} onClick={() => setModalVisible(true)}>
                  添加用户
                </Button>
              </Space>
            }
          >
            <UserTable
              users={users}
              loading={loading}
              onDelete={handleDeleteUser}
            />
          </Card>
        </Col>
      </Row>

      <Modal
        title={
          <Space>
            <UserAddOutlined style={{ color: '#1890ff' }} />
            <span>添加新用户</span>
          </Space>
        }
        open={modalVisible}
        onCancel={() => setModalVisible(false)}
        footer={null}
        width={600}
      >
        <AddUserForm onSubmit={handleAddUser} />
      </Modal>
    </div>
  );
};

export default UserManagement;
