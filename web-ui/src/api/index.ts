import axios from 'axios';
import { message } from 'antd';

export const ADMIN_AUTH_REQUIRED_EVENT = 'gps-admin-auth-required';

const api = axios.create({
  baseURL: '/api',
  timeout: 10000,
});

function extractErrorMessage(payload: unknown): string | null {
  if (typeof payload === 'string' && payload.trim() !== '') {
    return payload;
  }

  if (payload && typeof payload === 'object' && 'error' in payload) {
    const errorMessage = (payload as { error?: unknown }).error;
    if (typeof errorMessage === 'string' && errorMessage.trim() !== '') {
      return errorMessage;
    }
  }

  return null;
}

function translateErrorMessage(messageText: string): string {
  switch (messageText) {
    case 'invalid password':
      return '管理密码错误';
    case 'bootstrap required':
      return '请先初始化管理后台密码';
    case 'authentication required':
      return '请先登录管理后台';
    case 'admin password is already configured':
      return '管理后台密码已初始化';
    case 'invalid bootstrap token':
      return '初始化令牌无效';
    case 'admin password must be at least 8 characters':
      return '管理密码至少需要 8 个字符';
    case 'captcha verification required':
      return '请先完成验证码校验';
    case 'captcha verification failed':
      return '验证码校验失败，请重试';
    case 'captcha configuration incomplete':
      return '验证码配置不完整，请检查 GEETEST_ID 和 GEETEST_KEY';
    default:
      if (messageText.startsWith("route '") && messageText.includes("' already exists for client '")) {
        const matched = messageText.match(/^route '(.+)' already exists for client '(.+)'$/);
        if (matched) {
          return `客户端 ${matched[2]} 下已存在同名路由：${matched[1]}`;
        }
        return '当前客户端下已存在同名路由';
      }
      return messageText;
  }
}

export function getApiErrorMessage(error: unknown, fallback = '请求失败'): string {
  if (axios.isAxiosError(error)) {
    const rawMessage = extractErrorMessage(error.response?.data) || error.message || fallback;
    return translateErrorMessage(rawMessage);
  }

  if (error instanceof Error && error.message.trim() !== '') {
    return translateErrorMessage(error.message);
  }

  return fallback;
}

api.interceptors.response.use(
  (response) => response,
  (error) => {
    if (axios.isAxiosError(error) && error.response?.status === 401) {
      window.dispatchEvent(new Event(ADMIN_AUTH_REQUIRED_EVENT));
      return Promise.reject(error);
    }

    message.error(getApiErrorMessage(error));
    return Promise.reject(error);
  }
);

export default api;
