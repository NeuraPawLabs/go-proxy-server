import React from 'react';
import { Table, Button, Popconfirm, Space } from 'antd';
import { DeleteOutlined } from '@ant-design/icons';
import type { ColumnsType } from 'antd/es/table';

interface WhitelistTableProps {
  whitelist: string[];
  loading: boolean;
  onDelete: (ip: string) => void;
}

interface WhitelistItem {
  ip: string;
}

const WhitelistTable: React.FC<WhitelistTableProps> = ({ whitelist, loading, onDelete }) => {
  const dataSource: WhitelistItem[] = whitelist.map(ip => ({ ip }));

  const columns: ColumnsType<WhitelistItem> = [
    {
      title: 'IP 地址',
      dataIndex: 'ip',
      key: 'ip',
    },
    {
      title: '操作',
      key: 'action',
      render: (_, record) => (
        <Space>
          <Popconfirm
            title="确定要删除这个 IP 吗？"
            onConfirm={() => onDelete(record.ip)}
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
      dataSource={dataSource}
      loading={loading}
      rowKey="ip"
      pagination={{ pageSize: 10 }}
    />
  );
};

export default WhitelistTable;
