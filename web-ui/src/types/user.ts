export interface User {
  Username: string;
  IP: string;
  CreatedAt: string;
}

export interface AddUserRequest {
  username: string;
  password: string;
  ip?: string;
}

export interface DeleteUserRequest {
  username: string;
}
