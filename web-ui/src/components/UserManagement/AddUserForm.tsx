import React from 'react';
import { Form, Input, Button, Space, Divider } from 'antd';
import { UserOutlined, LockOutlined, GlobalOutlined } from '@ant-design/icons';
import type { AddUserRequest } from '../../types/user';

interface AddUserFormProps {
  onSubmit: (values: AddUserRequest) => void;
}

const AddUserForm: React.FC<AddUserFormProps> = ({ onSubmit }) => {
  const [form] = Form.useForm();

  const handleSubmit = (values: AddUserRequest) => {
    onSubmit(values);
    form.resetFields();
  };

  return (
    <div style={{ padding: '24px 0' }}>
      <Form
        form={form}
        layout="vertical"
        onFinish={handleSubmit}
        size="large"
        autoComplete="off"
      >
        <Form.Item
          label={<span style={{ fontSize: '15px', fontWeight: 500 }}>用户名</span>}
          name="username"
          rules={[
            { required: true, message: '请输入用户名' },
            { min: 3, message: '用户名至少3个字符' },
            { pattern: /^[a-zA-Z0-9_-]+$/, message: '只能包含字母、数字、下划线和连字符' }
          ]}
        >
          <Input
            prefix={<UserOutlined style={{ color: '#bfbfbf' }} />}
            placeholder="用户名"
            maxLength={20}
            showCount
          />
        </Form.Item>

        <Form.Item
          label={<span style={{ fontSize: '15px', fontWeight: 500 }}>密码</span>}
          name="password"
          rules={[
            { required: true, message: '请输入密码' },
            { min: 8, message: '密码至少8个字符' },
            { pattern: /^(?=.*[A-Za-z])(?=.*\d)/, message: '密码必须包含字母和数字' }
          ]}
        >
          <Input.Password
            prefix={<LockOutlined style={{ color: '#bfbfbf' }} />}
            placeholder="密码"
            maxLength={50}
          />
        </Form.Item>

        <Form.Item
          label={<span style={{ fontSize: '15px', fontWeight: 500 }}>IP 地址</span>}
          name="ip"
          rules={[
            { pattern: /^(\d{1,3}\.){3}\d{1,3}$/, message: '请输入有效的IP地址' }
          ]}
        >
          <Input
            prefix={<GlobalOutlined style={{ color: '#bfbfbf' }} />}
            placeholder="IP（可选）"
          />
        </Form.Item>

        <Divider style={{ margin: '24px 0' }} />

        <Form.Item style={{ marginBottom: 0 }}>
          <Space style={{ width: '100%', justifyContent: 'flex-end' }}>
            <Button onClick={() => form.resetFields()}>
              重置
            </Button>
            <Button type="primary" htmlType="submit" size="large">
              添加用户
            </Button>
          </Space>
        </Form.Item>
      </Form>
    </div>
  );
};

export default AddUserForm;
