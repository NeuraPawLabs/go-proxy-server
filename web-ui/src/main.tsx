import React from 'react';
import ReactDOM from 'react-dom/client';
import { ConfigProvider } from 'antd';
import zhCN from 'antd/locale/zh_CN';
import App from './App';
import './index.css';

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <ConfigProvider
      locale={zhCN}
      theme={{
        token: {
          colorPrimary: '#1890ff',
          colorSuccess: '#52c41a',
          colorWarning: '#faad14',
          colorError: '#ff4d4f',
          colorInfo: '#1890ff',
          borderRadius: 8,
          fontSize: 14,
          fontFamily: '-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif',
        },
        components: {
          Layout: {
            headerBg: '#ffffff',
            siderBg: '#001529',
            bodyBg: '#f0f2f5',
          },
          Menu: {
            darkItemBg: '#001529',
            darkItemSelectedBg: '#1890ff',
            darkItemHoverBg: 'rgba(255, 255, 255, 0.08)',
          },
          Card: {
            borderRadiusLG: 8,
            boxShadowTertiary: '0 2px 8px rgba(0, 0, 0, 0.1)',
          },
          Button: {
            borderRadius: 6,
            controlHeight: 36,
            controlHeightLG: 40,
          },
          Input: {
            borderRadius: 6,
            controlHeight: 36,
          },
          Table: {
            borderRadius: 8,
            headerBg: '#fafafa',
          },
        },
      }}
    >
      <App />
    </ConfigProvider>
  </React.StrictMode>,
);
