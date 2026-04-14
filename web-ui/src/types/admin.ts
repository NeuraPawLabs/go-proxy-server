export interface AdminSessionStatus {
  authenticated: boolean;
  bootstrapNeeded: boolean;
  geetestId?: string;
  captchaError?: string;
}

export interface LoginPayload {
  password: string;
  bootstrapToken?: string;
  lot_number?: string;
  captcha_output?: string;
  pass_token?: string;
  gen_time?: string;
}
