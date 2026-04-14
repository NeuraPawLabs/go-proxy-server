import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Alert, Button, Card, Form, Input, Space, Typography } from 'antd';
import { LockOutlined } from '@ant-design/icons';
import type { LoginPayload } from '../../types/admin';

const { Title } = Typography;

interface GeetestValidateResult {
  lot_number: string;
  captcha_output: string;
  pass_token: string;
  gen_time: string;
}

interface GeetestCaptcha {
  destroy?: () => void;
  getValidate: () => GeetestValidateResult | undefined;
  onReady?: (callback: () => void) => void;
  onSuccess?: (callback: () => void) => void;
  onError?: (callback: () => void) => void;
  onClose?: (callback: () => void) => void;
  reset?: () => void;
  showCaptcha?: () => void;
}

declare global {
  interface Window {
    initGeetest4?: (
      config: { captchaId: string; product: 'bind' },
      callback: (captcha: GeetestCaptcha) => void,
    ) => void;
  }
}

let geetestScriptPromise: Promise<void> | null = null;

function loadGeetestScript(): Promise<void> {
  if (window.initGeetest4) {
    return Promise.resolve();
  }
  if (geetestScriptPromise) {
    return geetestScriptPromise;
  }

  geetestScriptPromise = new Promise<void>((resolve, reject) => {
    const existingScript = document.querySelector<HTMLScriptElement>('script[data-geetest-script="true"]');
    if (existingScript) {
      existingScript.addEventListener('load', () => resolve(), { once: true });
      existingScript.addEventListener('error', () => reject(new Error('验证码加载失败，请刷新页面后重试')), {
        once: true,
      });
      return;
    }

    const script = document.createElement('script');
    script.src = 'https://static.geetest.com/v4/gt4.js';
    script.async = true;
    script.dataset.geetestScript = 'true';
    script.onload = () => resolve();
    script.onerror = () => {
      geetestScriptPromise = null;
      script.remove();
      reject(new Error('验证码加载失败，请刷新页面后重试'));
    };
    document.head.appendChild(script);
  });

  return geetestScriptPromise;
}

interface AdminAuthProps {
  bootstrapNeeded: boolean;
  submitting: boolean;
  error: string | null;
  geetestId?: string;
  captchaError?: string;
  onSubmit: (payload: LoginPayload) => Promise<void>;
}

interface AdminAuthFormValues {
  password: string;
  bootstrapToken?: string;
  confirmPassword?: string;
}

const AdminAuth: React.FC<AdminAuthProps> = ({
  bootstrapNeeded,
  submitting,
  error,
  geetestId,
  captchaError,
  onSubmit,
}) => {
  const [form] = Form.useForm<AdminAuthFormValues>();
  const [localError, setLocalError] = useState<string | null>(null);
  const [captchaReady, setCaptchaReady] = useState(() => bootstrapNeeded || !geetestId);
  const captchaRef = useRef<GeetestCaptcha | null>(null);
  const pendingValuesRef = useRef<AdminAuthFormValues | null>(null);

  const submitLogin = useCallback(async (
    values: AdminAuthFormValues,
    captchaResult?: GeetestValidateResult,
  ) => {
    const payload: LoginPayload = { password: values.password };

    if (bootstrapNeeded) {
      payload.bootstrapToken = values.bootstrapToken;
    }

    if (captchaResult) {
      payload.lot_number = captchaResult.lot_number;
      payload.captcha_output = captchaResult.captcha_output;
      payload.pass_token = captchaResult.pass_token;
      payload.gen_time = captchaResult.gen_time;
    }

    try {
      await onSubmit(payload);
    } catch (submitError) {
      captchaRef.current?.reset?.();
      throw submitError;
    }
  }, [onSubmit]);

  useEffect(() => {
    captchaRef.current?.destroy?.();
    captchaRef.current = null;
    pendingValuesRef.current = null;

    if (bootstrapNeeded || !geetestId) {
      return;
    }

    let cancelled = false;
    queueMicrotask(() => {
      if (!cancelled) {
        setCaptchaReady(false);
        setLocalError(null);
      }
    });

    void loadGeetestScript()
      .then(() => {
        if (cancelled) {
          return;
        }
        if (!window.initGeetest4) {
          throw new Error('验证码加载失败，请刷新页面后重试');
        }

        window.initGeetest4(
          {
            captchaId: geetestId,
            product: 'bind',
          },
          (captcha) => {
            if (cancelled) {
              captcha.destroy?.();
              return;
            }

            captchaRef.current = captcha;

            captcha.onReady?.(() => {
              if (!cancelled) {
                setCaptchaReady(true);
              }
            });

            captcha.onSuccess?.(() => {
              if (cancelled) {
                return;
              }

              const pendingValues = pendingValuesRef.current;
              if (!pendingValues) {
                return;
              }

              const result = captcha.getValidate();
              if (!result) {
                return;
              }

              pendingValuesRef.current = null;
              setLocalError(null);
              void submitLogin(pendingValues, result).catch(() => {});
            });

            captcha.onError?.(() => {
              if (!cancelled) {
                pendingValuesRef.current = null;
                setLocalError('验证码加载失败，请稍后重试');
              }
            });

            captcha.onClose?.(() => {
              if (!cancelled) {
                pendingValuesRef.current = null;
              }
            });

            if (typeof captcha.onReady !== 'function') {
              setCaptchaReady(true);
            }
          },
        );
      })
      .catch((loadError: unknown) => {
        if (cancelled) {
          return;
        }
        setLocalError(loadError instanceof Error ? loadError.message : '验证码加载失败，请刷新页面后重试');
      });

    return () => {
      cancelled = true;
      captchaRef.current?.destroy?.();
      captchaRef.current = null;
    };
  }, [bootstrapNeeded, geetestId, submitLogin]);

  const handleFinish = async (values: AdminAuthFormValues) => {
    setLocalError(null);

    if (!bootstrapNeeded && geetestId) {
      if (captchaError) {
        setLocalError(captchaError);
        return;
      }

      const captcha = captchaRef.current;
      if (!captcha || !captchaReady) {
        setLocalError('验证码正在加载，请稍后再试');
        return;
      }

      const result = captcha.getValidate();
      if (!result) {
        pendingValuesRef.current = values;
        captcha.showCaptcha?.();
        return;
      }
      await submitLogin(values, result);
      return;
    }

    await submitLogin(values);
  };

  const combinedError = localError ?? captchaError ?? error;
  const loginDisabled = Boolean(!bootstrapNeeded && (captchaError || (geetestId && !captchaReady)));

  return (
    <div className="admin-auth-shell">
      <Card className="admin-auth-card" variant="borderless">
        <Space direction="vertical" size={16} style={{ width: '100%' }}>
          <div className="admin-auth-header">
            <Title level={4} style={{ marginBottom: 0 }}>
              {bootstrapNeeded ? '初始化管理后台' : '登录管理后台'}
            </Title>
          </div>

          {combinedError ? <Alert type="error" showIcon message={combinedError} /> : null}

          <Form
            form={form}
            layout="vertical"
            size="large"
            autoComplete="off"
            onFinish={(values) => {
              void handleFinish(values).catch(() => {});
            }}
          >
            <Form.Item
              label={bootstrapNeeded ? '设置管理密码' : '管理密码'}
              name="password"
              rules={[
                { required: true, message: '请输入管理密码' },
                { min: 8, message: '密码至少需要 8 个字符' },
              ]}
            >
              <Input.Password
                prefix={<LockOutlined />}
                autoFocus
                autoComplete={bootstrapNeeded ? 'new-password' : 'current-password'}
                placeholder={bootstrapNeeded ? '请输入新的管理密码' : '请输入管理密码'}
              />
            </Form.Item>

            {bootstrapNeeded ? (
              <Form.Item
                label="初始化令牌"
                name="bootstrapToken"
                rules={[
                  { required: true, message: '请输入初始化令牌' },
                ]}
                extra="请输入服务端启动日志中打印的一次性 bootstrap token。"
              >
                <Input.Password
                  prefix={<LockOutlined />}
                  autoComplete="one-time-code"
                  placeholder="请输入 bootstrap token"
                />
              </Form.Item>
            ) : null}

            {bootstrapNeeded ? (
              <Form.Item
                label="确认管理密码"
                name="confirmPassword"
                dependencies={['password']}
                rules={[
                  { required: true, message: '请再次输入管理密码' },
                  ({ getFieldValue }) => ({
                    validator(_, value) {
                      if (!value || getFieldValue('password') === value) {
                        return Promise.resolve();
                      }
                      return Promise.reject(new Error('两次输入的密码不一致'));
                    },
                  }),
                ]}
              >
                <Input.Password
                  prefix={<LockOutlined />}
                  autoComplete="new-password"
                  placeholder="请再次输入管理密码"
                />
              </Form.Item>
            ) : null}
            <Button type="primary" htmlType="submit" block loading={submitting} disabled={loginDisabled}>
              {bootstrapNeeded ? '完成初始化并进入后台' : '登录后台'}
            </Button>
          </Form>
        </Space>
      </Card>
    </div>
  );
};

export default AdminAuth;
