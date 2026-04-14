import React from 'react';
import { Form, Input, Button } from 'antd';

interface AddIPFormProps {
  onSubmit: (ip: string) => void;
}

const AddIPForm: React.FC<AddIPFormProps> = ({ onSubmit }) => {
  const [form] = Form.useForm();

  const handleSubmit = (values: { ip: string }) => {
    onSubmit(values.ip);
    form.resetFields();
  };

  return (
    <Form
      form={form}
      layout="inline"
      onFinish={handleSubmit}
    >
      <Form.Item
        name="ip"
        rules={[
          { required: true, message: '请输入 IP 地址' },
          {
            pattern: /^(\d{1,3}\.){3}\d{1,3}$/,
            message: '请输入有效的 IP 地址',
          },
        ]}
      >
        <Input placeholder="IP 地址（例如：192.168.1.100）" style={{ width: 250 }} />
      </Form.Item>

      <Form.Item>
        <Button type="primary" htmlType="submit">
          添加 IP
        </Button>
      </Form.Item>
    </Form>
  );
};

export default AddIPForm;
