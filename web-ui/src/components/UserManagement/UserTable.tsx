import React from 'react';
import { Table, Button, Popconfirm, Space } from 'antd';
import { DeleteOutlined } from '@ant-design/icons';
import type { User } from '../../types/user';
import type { ColumnsType } from 'antd/es/table';

interface UserTableProps {
  users: User[];
  loading: boolean;
  onDelete: (username: string) => void;
}

const UserTable: React.FC<UserTableProps> = ({ users, loading, onDelete }) => {
  const columns: ColumnsType<User> = [
    {
      title: '用户名',
      dataIndex: 'Username',
      key: 'username',
    },
    {
      title: 'IP 地址',
      dataIndex: 'IP',
      key: 'ip',
      render: (ip: string) => ip || '不限制',
    },
    {
      title: '创建时间',
      dataIndex: 'CreatedAt',
      key: 'createdAt',
      render: (date: string) => new Date(date).toLocaleString('zh-CN'),
    },
    {
      title: '操作',
      key: 'action',
      render: (_, record) => (
        <Space>
          <Popconfirm
            title="确定要删除这个用户吗？"
            onConfirm={() => onDelete(record.Username)}
            okText="确定"
            cancelText="取消"
          >
            <Button type="link" danger icon={<DeleteOutlined />}>
              删除
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <Table
      columns={columns}
      dataSource={users}
      loading={loading}
      rowKey="Username"
      pagination={{ pageSize: 10 }}
    />
  );
};

export default UserTable;
